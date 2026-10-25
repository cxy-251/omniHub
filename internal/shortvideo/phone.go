package shortvideo

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
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
}

// endMark 标在清单末尾：读到它才说明手机把清单完整输出了（数据线中途断开时输出会被截断）。
const endMark = "__OMNIHUB_END__"

// recordMark 在合并输出的 downloaded.txt 里分隔各文件（记录行是「平台 作品ID」，不会以它开头）。
const recordMark = "==omnihub== "

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

// PhoneListing 是手机 shortvideo/ 的文件清单与全部下载记录。手机共享存储不区分大小写，按小写比对。
type PhoneListing struct {
	sizes map[string]int64
	dirs  map[string]bool
	recs  map[string][]string // 小写 rel → 记录行
	names map[string]string   // 小写 rel → 手机上的原名
}

func (l *PhoneListing) isDir(rel string) bool { return l.dirs[strings.ToLower(rel)] }
func (l *PhoneListing) size(rel string) (int64, bool) {
	n, ok := l.sizes[strings.ToLower(rel)]
	return n, ok
}
func (l *PhoneListing) records(rel string) ([]string, error) { return l.recs[strings.ToLower(rel)], nil }
func (l *PhoneListing) recordFiles() ([]string, error) {
	var out []string
	for k := range l.recs {
		out = append(out, l.names[k])
	}
	return out, nil
}

// List 读出手机上的文件清单和所有 <平台>/<博主>/downloaded.txt。
func (p Phone) List(ctx context.Context) (*PhoneListing, error) {
	l := &PhoneListing{sizes: map[string]int64{}, dirs: map[string]bool{}, recs: map[string][]string{}, names: map[string]string{}}
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
		l.sizes[key], l.names[key] = n, rel
		for d := path.Dir(key); d != "."; d = path.Dir(d) {
			l.dirs[d] = true
		}
	}
	out, err = p.Shell(ctx, "[ -d "+shq(p.Dir)+" ] && cd "+shq(p.Dir)+" && for f in */*/"+ArchiveFile+`; do [ -f "$f" ] && { echo "`+recordMark+`$f"; cat "$f"; echo; }; done; echo `+endMark)
	if err != nil {
		return nil, fmt.Errorf("读手机下载记录：%w", err)
	}
	if lines, err = complete(out); err != nil {
		return nil, err
	}
	key := ""
	for _, line := range lines {
		if rel, ok := strings.CutPrefix(line, recordMark); ok {
			key = strings.ToLower(rel)
			l.recs[key], l.names[key] = []string{}, rel
		} else if t := strings.TrimSpace(line); t != "" && key != "" {
			l.recs[key] = append(l.recs[key], t)
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

// PlanPhone 列出要补到手机的内容：来源的所有博主都补（手机放全量），downloaded.txt 两边合并后一致。
func PlanPhone(ctx context.Context, src string, phone Phone) (CopyPlan, *PhoneListing, error) {
	l, err := phone.List(ctx)
	if err != nil {
		return CopyPlan{}, nil, err
	}
	plan, err := planCopy(src, l, false)
	if err != nil {
		return plan, nil, err
	}
	// downloaded.txt 不当普通文件传：合并回来源后，和手机不一致的在最后整个推过去。Archives 列出这些
	files := plan.Files[:0]
	for _, f := range plan.Files {
		if path.Base(f.Rel) == ArchiveFile {
			plan.Bytes -= f.Size
		} else {
			files = append(files, f)
		}
	}
	plan.Files, plan.Count = files, len(files)
	plan.Archives = nil
	found, err := filepath.Glob(filepath.Join(src, "*", "*", ArchiveFile))
	if err != nil {
		return plan, nil, err
	}
	for _, f := range found {
		rel, _ := filepath.Rel(src, f)
		rel = filepath.ToSlash(rel)
		mine, err := archiveLines(f)
		if err != nil {
			return plan, nil, err
		}
		theirs, _ := l.records(rel)
		if _, exists := l.size(rel); !exists || !slices.Equal(append(mine, linesMissing(theirs, mine)...), theirs) {
			plan.Archives = append(plan.Archives, rel)
		}
	}
	// 只在手机上有记录的博主：合并回来源后两边内容相同，不用推
	return plan, l, nil
}

// RunPhone 按计划同步到手机：
//  1. 手机上的下载记录先合并回来源（来源要保存全部记录）；
//  2. 文件先推到手机的 .omnihub-incoming 临时目录，核对大小后改名放进正式位置，手机上已有的同名文件不覆盖；
//     中断只会在临时目录留下半截文件，下次开始时清掉；
//  3. 来源里和手机不一致的 downloaded.txt 整个推过去替换（此时来源已包含手机的全部记录），两边内容一致。
func RunPhone(ctx context.Context, src string, phone Phone, plan CopyPlan, l *PhoneListing, progress func(done int, bytes int64)) (CopyResult, error) {
	var res CopyResult
	for _, rel := range plan.Back {
		theirs, _ := l.records(rel)
		if _, err := mergeLines(theirs, filepath.Join(src, filepath.FromSlash(rel))); err != nil {
			return res, fmt.Errorf("%s：%w", rel, err)
		}
		res.Back++
	}
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
			var err error
			for try := 0; try < 3; try++ {
				if err = ctx.Err(); err != nil {
					return res, err
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
				time.Sleep(3 * time.Second)
			}
			if err != nil {
				return res, fmt.Errorf("%s：%w", dir, err)
			}
		}
	}
	n, err := phone.pushRecords(ctx, src, l)
	res.Archives = n
	if err != nil {
		return res, err
	}
	_, _ = phone.Shell(ctx, "rm -rf "+shq(inc))
	return res, nil
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
			}
		}
		if len(bad) > 0 {
			return moved, fmt.Errorf("%d 个文件传过去大小不对", len(bad))
		}
	}
	return moved, nil
}

// pushRecords 把来源里和手机不一致（记录或顺序不同）的 downloaded.txt 推过去替换，返回推过去的个数。
func (p Phone) pushRecords(ctx context.Context, src string, l *PhoneListing) (int, error) {
	found, err := filepath.Glob(filepath.Join(src, "*", "*", ArchiveFile))
	if err != nil {
		return 0, err
	}
	stage, err := os.MkdirTemp("", "omnihub-records-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(stage)
	root := filepath.Join(stage, "records")
	var rels []string
	for _, f := range found {
		rel, _ := filepath.Rel(src, f)
		rel = filepath.ToSlash(rel)
		mine, err := archiveLines(f)
		if err != nil {
			return 0, err
		}
		theirs, _ := l.records(rel)
		_, exists := l.size(rel)
		if exists && slices.Equal(mine, theirs) { // 顺序也一样才算一致：替换后两边是同一份文件
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			return 0, err
		}
		to := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return 0, err
		}
		if err := os.WriteFile(to, data, 0o644); err != nil {
			return 0, err
		}
		rels = append(rels, rel)
	}
	if len(rels) == 0 {
		return 0, nil
	}
	inc := p.incoming()
	if _, err := p.Shell(ctx, "rm -rf "+shq(path.Join(inc, "records"))); err != nil {
		return 0, err
	}
	pctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	err = p.Push(pctx, []string{root}, inc)
	cancel()
	if err != nil {
		return 0, fmt.Errorf("推送下载记录：%w", err)
	}
	done := 0
	for i := 0; i < len(rels); i += pushBatch {
		var script strings.Builder
		for _, rel := range rels[i:min(i+pushBatch, len(rels))] {
			to := path.Join(p.Dir, rel)
			if name, ok := l.names[strings.ToLower(rel)]; ok {
				to = path.Join(p.Dir, name) // 手机上已有的用它的原名，避免大小写不同变成两个文件
			}
			fmt.Fprintf(&script, "mkdir -p %s && mv -f %s %s && echo ok\n", shq(path.Dir(to)), shq(path.Join(inc, "records", rel)), shq(to))
		}
		out, err := p.Shell(ctx, script.String())
		done += strings.Count(string(out), "ok\n")
		if err != nil {
			return done, fmt.Errorf("替换手机上的下载记录：%w", err)
		}
	}
	return done, nil
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
func PlanClean(src string, phone *PhoneListing, keep []string) (CleanPlan, error) {
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
		if err != nil || !st.IsDir() || skipName(filepath.Base(c)) || skipName(filepath.Base(filepath.Dir(c))) {
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
