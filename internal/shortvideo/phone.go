package shortvideo

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Phone 是经数据线（adb）连着的手机上的 shortvideo/ 目录。Shell 在手机上执行一段 sh 脚本，
// Push 把本机文件（或一个目录）推到手机目录下；测试里两者用本机的 sh 与 cp 代替。
type Phone struct {
	Dir   string // 手机上 shortvideo/ 的绝对路径
	Shell func(ctx context.Context, script string) ([]byte, error)
	Push  func(ctx context.Context, files []string, remoteDir string) error
	// Reserve 是要给手机留的剩余空间，0 时用 PhoneReserve
	Reserve int64
	// Reconnect 在断线后等手机重新连上（可能要等用户拔插数据线）；为空时只稍等再重试
	Reconnect func(ctx context.Context) error
	// Waiting 在等待重新连接时调用，告诉界面现在在等什么（空字符串表示等完了）
	Waiting func(note string)
}

// endMark 标在清单末尾：读到它才说明手机把清单完整输出了（数据线中途断开时输出会被截断）。
const endMark = "__OMNIHUB_END__"

// ErrPhoneFull 表示手机剩余空间不够，视频传到一半停下了。
var ErrPhoneFull = errors.New("手机剩余空间不够，视频没传完就停下了")

// PhoneReserve 是同步时给手机留的剩余空间：传完一批会低于它就停下（存储塞满时安卓会出问题）。
const PhoneReserve = 10 << 30

// pushBatch 是一次 adb push 与一次核对改名脚本处理的文件数上限（脚本长度随之增长）。
const pushBatch = 50

// ConnectADB 确认手机经数据线连着（adb 认得、已授权），把 USB 模式设为仅充电，返回能用的 Phone。
// 手机 USB 模式为文件传输（MTP）时，桌面会自动打开手机、和 adb 抢占 USB；仅充电不影响 USB 调试。
func ConnectADB(ctx context.Context, adb, dir string) (Phone, error) {
	run := func(ctx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, adb, append([]string{"-d"}, args...)...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			if msg := strings.TrimSpace(stderr.String()); msg != "" {
				err = fmt.Errorf("%w：%s", err, msg)
			}
			return nil, err
		}
		return bytes.ReplaceAll(out, []byte("\r\n"), []byte("\n")), nil
	}
	p := Phone{
		Dir:   dir,
		Shell: func(ctx context.Context, script string) ([]byte, error) { return run(ctx, "shell", script) },
		Push: func(ctx context.Context, files []string, remoteDir string) error {
			_, err := run(ctx, append(append([]string{"push"}, files...), remoteDir+"/")...)
			return err
		},
	}
	// 断线重连（照 z6x 的 phone_push.sh）：先等手机重新出现，认不回来就重启 adb 服务；
	// 手机端 adbd 卡死时只能拔插数据线（或关开 USB 调试），最多等 30 分钟
	p.Reconnect = func(ctx context.Context) error {
		ready := func(d time.Duration) bool {
			c, cancel := context.WithTimeout(ctx, d)
			defer cancel()
			if _, err := run(c, "wait-for-usb-device"); err != nil {
				return false
			}
			out, err := run(c, "shell", "echo ok")
			return err == nil && strings.TrimSpace(string(out)) == "ok"
		}
		for range 3 {
			if ready(60 * time.Second) {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			c, cancel := context.WithTimeout(ctx, 20*time.Second)
			_ = exec.CommandContext(c, adb, "kill-server").Run()
			_ = exec.CommandContext(c, adb, "start-server").Run()
			cancel()
		}
		if p.Waiting != nil {
			p.Waiting("手机的 adb 卡住了：拔下数据线再插上（或在开发者选项里关掉再打开「USB 调试」），插回后自动接着传，最多等 30 分钟")
			defer p.Waiting("")
		}
		if !ready(30 * time.Minute) {
			return errors.New("手机一直没有重新连上")
		}
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		_, _ = run(c, "shell", "svc usb setFunctions")
		time.Sleep(3 * time.Second)
		if !ready(60 * time.Second) {
			return errors.New("手机一直没有重新连上")
		}
		return nil
	}
	short, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if out, err := run(short, "get-state"); err != nil || strings.TrimSpace(string(out)) != "device" {
		return p, errors.New("手机没连上：用数据线直接插 Deck 机身的 USB-C 口，打开手机的「USB 调试」并允许这台电脑")
	}
	if _, err := run(short, "shell", "svc usb setFunctions"); err == nil {
		wait, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		_, _ = run(wait, "wait-for-usb-device")
	}
	if out, err := p.Shell(short, "[ -d "+shq(path.Dir(dir))+" ] && echo ok"); err != nil || strings.TrimSpace(string(out)) != "ok" {
		return p, fmt.Errorf("手机上没有 %s", path.Dir(dir))
	}
	return p, nil
}

// shq 把字符串转成 sh 的单引号形式。
func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func (p Phone) incoming() string { return path.Join(path.Dir(p.Dir), ".omnihub-incoming") }

// Free 返回手机上 shortvideo/ 所在存储的剩余字节数。
func (p Phone) Free(ctx context.Context) (int64, error) {
	out, err := p.Shell(ctx, "stat -f -c '%a %S' "+shq(path.Dir(p.Dir)))
	if err != nil {
		return 0, fmt.Errorf("查手机剩余空间：%w", err)
	}
	var blocks, size int64
	if _, err := fmt.Sscan(string(out), &blocks, &size); err != nil {
		return 0, fmt.Errorf("查手机剩余空间：输出看不懂（%q）", strings.TrimSpace(string(out)))
	}
	return blocks * size, nil
}

// PhoneListing 是手机 shortvideo/ 的文件清单与全部下载记录。手机共享存储不区分大小写，按小写比对。
type PhoneListing struct {
	sizes map[string]int64
	dirs  map[string]bool
}

func (l *PhoneListing) isDir(rel string) bool { return l.dirs[strings.ToLower(rel)] }
func (l *PhoneListing) size(rel string) (int64, bool) {
	n, ok := l.sizes[strings.ToLower(rel)]
	return n, ok
}

// List 读出手机上的文件清单。
func (p Phone) List(ctx context.Context) (*PhoneListing, error) {
	l := &PhoneListing{sizes: map[string]int64{}, dirs: map[string]bool{}}
	out, err := p.Shell(ctx, "[ -d "+shq(p.Dir)+" ] && cd "+shq(p.Dir)+" && find . -type f -printf '%P\\t%s\\n'; echo "+endMark)
	if err != nil {
		return nil, fmt.Errorf("读手机文件清单：%w", err)
	}
	lines, err := complete(out)
	if err != nil {
		return nil, err
	}
	for _, line := range lines {
		rel, size, ok := strings.Cut(line, "\t")
		n, err := strconv.ParseInt(size, 10, 64)
		if !ok || err != nil {
			continue
		}
		key := strings.ToLower(rel)
		l.sizes[key] = n
		for d := path.Dir(key); d != "."; d = path.Dir(d) {
			l.dirs[d] = true
		}
	}
	return l, nil
}

// complete 拆行并确认末尾有结束标记。
func complete(out []byte) ([]string, error) {
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) == 0 || lines[len(lines)-1] != endMark {
		return nil, errors.New("手机的输出不完整（数据线可能断了），请重试")
	}
	return lines[:len(lines)-1], nil
}

// PlanPhone 列出要补到手机的内容：来源的所有博主都补（手机放全量）。
// platforms 不为空时只处理这些平台（见 PlanCopy）。
func PlanPhone(ctx context.Context, src string, phone Phone, platforms []string) (CopyPlan, *PhoneListing, error) {
	l, err := phone.List(ctx)
	if err != nil {
		return CopyPlan{}, nil, err
	}
	plan, err := planCopy(src, l, false, platforms)
	if err != nil {
		return plan, nil, err
	}
	if plan.PhoneFree, err = phone.Free(ctx); err != nil {
		return plan, nil, err
	}
	return plan, l, nil
}

// RunPhone 按计划同步到手机：文件先推到手机的 .omnihub-incoming 临时目录，核对大小后改名放进正式位置，手机上已有的同名文件不覆盖；
// 中断只会在临时目录留下半截文件，下次开始时清掉。
func RunPhone(ctx context.Context, src string, phone Phone, plan CopyPlan, l *PhoneListing, progress func(done int, bytes int64)) (CopyResult, error) {
	var res CopyResult
	inc := phone.incoming()
	if _, err := phone.Shell(ctx, "rm -rf "+shq(inc)+" && mkdir -p "+shq(inc)); err != nil {
		return res, fmt.Errorf("建手机上的临时目录：%w", err)
	}
	groups, order := map[string][]CopyFile{}, []string{}
	for _, f := range plan.Files {
		dir := path.Dir(f.Rel)
		if groups[dir] == nil {
			order = append(order, dir)
		}
		groups[dir] = append(groups[dir], f)
	}
	for _, dir := range order {
		files := groups[dir]
		for i := 0; i < len(files); i += pushBatch {
			batch := files[i:min(i+pushBatch, len(files))]
			var need int64
			for _, f := range batch {
				need += f.Size
			}
			// 断线（传输失败、读不到手机）时等手机重新连上，接着传这一批；已经放进手机的文件不会重传
			var err error
			for try := 0; ; try++ {
				if err = ctx.Err(); err != nil {
					return res, err
				}
				var free int64
				if free, err = phone.Free(ctx); err == nil {
					reserve := cmp.Or(phone.Reserve, PhoneReserve)
					if free-need < reserve {
						phone.cleanup(ctx)
						return res, fmt.Errorf("%w：还剩 %.1f GB，要给手机留 %.0f GB", ErrPhoneFull, float64(free)/(1<<30), float64(reserve)/(1<<30))
					}
					var moved []CopyFile
					if moved, err = phone.pushBatch(ctx, src, dir, batch); err == nil {
						for _, f := range moved {
							res.Copied++
							res.Bytes += f.Size
						}
						if progress != nil {
							progress(res.Copied, res.Bytes)
						}
						break
					}
				}
				if try >= 4 || ctx.Err() != nil {
					return res, fmt.Errorf("%s：%w", dir, err)
				}
				if phone.Reconnect == nil {
					time.Sleep(3 * time.Second)
				} else if rerr := phone.Reconnect(ctx); rerr != nil {
					return res, fmt.Errorf("%s：%w（%v）", dir, rerr, err)
				}
			}
		}
	}
	phone.cleanup(ctx)
	return res, nil
}

// cleanup 清掉手机上的临时目录。
func (p Phone) cleanup(ctx context.Context) {
	_, _ = p.Shell(ctx, "rm -rf "+shq(p.incoming()))
}

// pushBatch 把同一目录下的一批文件推到临时目录，核对大小后改名放进 dir；返回放进去的文件。
func (p Phone) pushBatch(ctx context.Context, src, dir string, batch []CopyFile) ([]CopyFile, error) {
	var total int64
	locals := make([]string, len(batch))
	for i, f := range batch {
		locals[i] = filepath.Join(src, filepath.FromSlash(f.Rel))
		total += f.Size
	}
	tmp, dst := path.Join(p.incoming(), "files"), path.Join(p.Dir, dir)
	if _, err := p.Shell(ctx, "rm -rf "+shq(tmp)+" && mkdir -p "+shq(tmp)+" "+shq(dst)); err != nil {
		return nil, err
	}
	// 超时：至少 2 分钟，按 5MB/s 估算，另加每个文件 0.1 秒
	pctx, cancel := context.WithTimeout(ctx, 2*time.Minute+time.Duration(total/5e6)*time.Second+time.Duration(len(batch))*100*time.Millisecond)
	err := p.Push(pctx, locals, tmp)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("传输中断：%w", err)
	}
	var script strings.Builder
	script.WriteString("cd " + shq(tmp) + " || exit 1\n")
	for _, f := range batch {
		name := path.Base(f.Rel)
		to := shq(path.Join(dst, name))
		fmt.Fprintf(&script, "[ \"$(stat -c %%s %s 2>/dev/null)\" = %d ] && [ ! -e %s ] && mv %s %s && echo %s\n", shq(name), f.Size, to, shq(name), to, shq(name))
	}
	out, err := p.Shell(ctx, script.String())
	if err != nil {
		return nil, err
	}
	ok := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		ok[line] = true
	}
	var moved, missing []CopyFile
	for _, f := range batch {
		if ok[path.Base(f.Rel)] {
			moved = append(moved, f)
		} else {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		// 没放进去的：大小不对（传坏了）就重传；手机上已经有了（同名，可能大小写不同）则不算错
		var bad []CopyFile
		for _, f := range missing {
			if out, _ := p.Shell(ctx, "[ -e "+shq(path.Join(dst, path.Base(f.Rel)))+" ] && echo y"); strings.TrimSpace(string(out)) != "y" {
				bad = append(bad, f)
			} else {
				moved = append(moved, f) // 计划时手机上没有，现在有了：是断线前那一轮放进去的
			}
		}
		if len(bad) > 0 {
			return moved, fmt.Errorf("%d 个文件传过去大小不对", len(bad))
		}
	}
	return moved, nil
}

// CleanPlan 是清理来源（SSD）里已同步视频的计划。Items 是要移进回收站的文件或整个图集目录。
type CleanPlan struct {
	Items      []string `json:"-"`
	Files      int      `json:"files"`
	Bytes      int64    `json:"bytes"`
	NotOnPhone int      `json:"not_on_phone"` // 手机上没有或大小不同：保留
	NotOnKeep  int      `json:"not_on_keep"`  // 其他库（SD 卡）有这个博主但还没补上：保留
	Creators   int      `json:"creators"`
}

// PlanClean 找出来源里可以清理的视频与图集：手机上有同名同大小的文件，且 keep 里每个已有这个博主的库也有。
// downloaded.txt、数据库、隐藏与半截文件一律保留。
// platforms 不为空时只清理这些平台（MV 等 omniHub 自己下载的不碰）。
func PlanClean(src string, phone *PhoneListing, keep []string, platforms []string) (CleanPlan, error) {
	var plan CleanPlan
	targets := make([]target, len(keep))
	for i, k := range keep {
		targets[i] = localTarget(k)
	}
	synced := func(rel string, size int64) bool {
		if n, ok := phone.size(rel); !ok || n != size {
			plan.NotOnPhone++
			return false
		}
		creator := strings.Join(strings.SplitN(rel, "/", 3)[:2], "/")
		for _, t := range targets {
			if !t.isDir(creator) {
				continue
			}
			if n, ok := t.size(rel); !ok || n != size {
				plan.NotOnKeep++
				return false
			}
		}
		return true
	}
	creators, err := filepath.Glob(filepath.Join(src, "*", "*"))
	if err != nil {
		return plan, err
	}
	for _, c := range creators {
		st, err := os.Lstat(c)
		if err != nil || !st.IsDir() || skipName(filepath.Base(c)) || skipName(filepath.Base(filepath.Dir(c))) || !inPlatforms(filepath.Base(filepath.Dir(c)), platforms) {
			continue
		}
		entries, err := os.ReadDir(c)
		if err != nil {
			return plan, err
		}
		before := plan.Files
		for _, e := range entries {
			name := e.Name()
			if name == ArchiveFile || skipName(name) {
				continue
			}
			abs := filepath.Join(c, name)
			rel, _ := filepath.Rel(src, abs)
			rel = filepath.ToSlash(rel)
			if e.Type().IsRegular() {
				if info, err := e.Info(); err == nil && synced(rel, info.Size()) {
					plan.Items = append(plan.Items, abs)
					plan.Files++
					plan.Bytes += info.Size()
				}
				continue
			}
			if !e.IsDir() {
				continue
			}
			// 图集或视频子目录：里面全部已同步就整个目录移走，否则只移已同步的文件
			var ok []string
			var okBytes int64
			all := true
			_ = filepath.WalkDir(abs, func(p string, d os.DirEntry, err error) error {
				if err != nil {
					all = false
					return nil
				}
				if d.IsDir() {
					return nil
				}
				info, ierr := d.Info()
				r, _ := filepath.Rel(src, p)
				if ierr != nil || !d.Type().IsRegular() || skipName(d.Name()) || !synced(filepath.ToSlash(r), info.Size()) {
					all = false
					return nil
				}
				ok = append(ok, p)
				okBytes += info.Size()
				return nil
			})
			if all && len(ok) > 0 {
				plan.Items = append(plan.Items, abs)
			} else {
				plan.Items = append(plan.Items, ok...)
			}
			plan.Files += len(ok)
			plan.Bytes += okBytes
		}
		if plan.Files > before {
			plan.Creators++
		}
	}
	return plan, nil
}

// RunClean 把计划里的文件与目录逐个移进回收站；返回移走的项数。
func RunClean(ctx context.Context, plan CleanPlan, trash func(string) error, progress func(done int)) (int, error) {
	for i, item := range plan.Items {
		if err := ctx.Err(); err != nil {
			return i, err
		}
		if err := trash(item); err != nil {
			return i, fmt.Errorf("%s：%w", filepath.Base(item), err)
		}
		if progress != nil {
			progress(i + 1)
		}
	}
	return len(plan.Items), nil
}
