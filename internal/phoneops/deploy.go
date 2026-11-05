package phoneops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type SSHOptions struct {
	User       string `json:"ssh_user"`
	Key        string `json:"key_file"`
	Port       int    `json:"ssh_port"`
	Install    string `json:"install_dir"`
	ServerPort int    `json:"server_port"`
}
type SSH struct {
	Program string
	Args    []string
	Close   func()
}

func (s SSH) Shell(ctx context.Context, script string) ([]byte, error) {
	return command(ctx, s.Program, append(append([]string{}, s.Args...), "sh -c "+Quote(script)), nil)
}
func (s SSH) Append(ctx context.Context, script string, in io.Reader) error {
	_, err := command(ctx, s.Program, append(append([]string{}, s.Args...), "sh -c "+Quote(script)), in)
	return err
}

var sshUser = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]{0,63}$`)

// ConnectSSH binds an ephemeral localhost forward to the selected ADB transport.
// Host keys are pinned by physical device identity, independently of IP or port.
func ConnectSSH(ctx context.Context, a ADB, identity, ssh, stateDir string, opt SSHOptions) (SSH, error) {
	if opt.Port == 0 {
		opt.Port = 8022
	}
	if opt.Port < 1 || opt.Port > 65535 {
		return SSH{}, errors.New("SSH 端口无效")
	}
	if opt.User == "" {
		out, err := a.Shell(ctx, "pm list packages -U com.termux")
		if err != nil {
			return SSH{}, err
		}
		for _, line := range strings.Split(string(out), "\n") {
			f := strings.Fields(line)
			if len(f) == 2 && f[0] == "package:com.termux" {
				uid, e := strconv.Atoi(strings.TrimPrefix(f[1], "uid:"))
				if e == nil && uid >= 10000 && uid < 100000 {
					opt.User = fmt.Sprintf("u0_a%d", uid-10000)
				}
			}
		}
	}
	if !sshUser.MatchString(opt.User) {
		return SSH{}, errors.New("无法确定 Termux 用户，请在高级设置填写 Termux 中 whoami 的结果")
	}
	if opt.Key != "" {
		st, err := os.Stat(opt.Key)
		if err != nil || !st.Mode().IsRegular() {
			return SSH{}, errors.New("SSH 私钥文件不可用")
		}
	}
	out, err := a.Run(ctx, "forward", "tcp:0", fmt.Sprintf("tcp:%d", opt.Port))
	if err != nil {
		return SSH{}, err
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || port < 1 || port > 65535 {
		return SSH{}, errors.New("ADB 未返回有效转发端口")
	}
	close := func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = a.Run(c, "forward", "--remove", fmt.Sprintf("tcp:%d", port))
	}
	if err = os.MkdirAll(stateDir, 0700); err != nil {
		close()
		return SSH{}, err
	}
	args := []string{"-F", os.DevNull, "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=5", "-o", "ServerAliveCountMax=3", "-o", "StrictHostKeyChecking=accept-new", "-o", "HostKeyAlias=omnihub-" + Token(identity), "-o", "UserKnownHostsFile=" + filepath.Join(stateDir, "known_hosts"), "-p", strconv.Itoa(port), "-l", opt.User}
	if opt.Key != "" {
		args = append(args, "-o", "IdentitiesOnly=yes", "-i", opt.Key)
	}
	args = append(args, "127.0.0.1")
	return SSH{Program: ssh, Args: args, Close: close}, nil
}
func InstallPath(ctx context.Context, s SSH, dir string) (string, error) {
	out, err := s.Shell(ctx, "printf '%s' \"$HOME\"")
	if err != nil {
		return "", fmt.Errorf("Termux SSH 未就绪，请先启动 sshd 并授权本机公钥：%w", err)
	}
	home := strings.TrimSpace(string(out))
	if !validAbsolute(home) {
		return "", errors.New("无法读取 Termux HOME")
	}
	if dir == "" {
		dir = path.Join(home, "omniHub")
	} else if !strings.HasPrefix(dir, "/") {
		if !validRel(dir) {
			return "", errors.New("安装目录无效")
		}
		dir = path.Join(home, dir)
	}
	if !validAbsolute(dir) || !strings.HasPrefix(dir, home+"/") {
		return "", errors.New("安装目录必须位于 Termux HOME 内")
	}
	if _, err = s.Shell(ctx, SafeDirs(home, strings.TrimPrefix(dir, home+"/"), false)); err != nil {
		return "", err
	}
	return dir, nil
}

// Deploy stages verified files, leaves config/media intact and rolls back on failed health checks.
// The commit shell survives a lost SSH connection; its result file can be inspected on reconnection.
func Deploy(ctx context.Context, s SSH, dir, packageDir string, port int, note func(string)) error {
	stamp := fmt.Sprintf("%d", time.Now().UnixNano())
	stage := path.Join(dir, ".deploy-"+stamp)
	if _, err := s.Shell(ctx, "mkdir -p "+Quote(dir)+" && test ! -L "+Quote(dir)+" && mkdir "+Quote(stage)); err != nil {
		return err
	}
	for _, name := range []string{"omnihub-server", "omnihub.sh"} {
		note("上传 " + name)
		f, err := os.Open(filepath.Join(packageDir, name))
		if err != nil {
			return err
		}
		h := sha256.New()
		if _, err = io.Copy(h, f); err != nil {
			f.Close()
			return err
		}
		if _, err = f.Seek(0, io.SeekStart); err != nil {
			f.Close()
			return err
		}
		err = s.Append(ctx, "cat > "+Quote(path.Join(stage, name)), f)
		f.Close()
		if err != nil {
			return err
		}
		remote, err := RemoteHash(ctx, s, path.Join(stage, name))
		if err != nil || remote != hex.EncodeToString(h.Sum(nil)) {
			return errors.New("手机安装包校验失败，原版本未改动")
		}
	}
	// Verify the actual executable before stopping the running service.
	if _, err := s.Shell(ctx, "chmod 700 "+Quote(path.Join(stage, "omnihub-server"))+" "+Quote(path.Join(stage, "omnihub.sh"))+" && "+Quote(path.Join(stage, "omnihub-server"))+" --version"); err != nil {
		return fmt.Errorf("新服务端不能在此设备运行，原版本未改动：%w", err)
	}
	note("文件校验通过，准备切换版本")
	if port == 0 {
		port = 8998
	}
	if port < 1 || port > 65535 {
		return errors.New("手机服务端口无效")
	}
	if _, err := s.Shell(ctx, "command -v curl >/dev/null"); err != nil {
		return errors.New("Termux 缺少 curl，请运行 pkg install curl 后再更新")
	}
	script := deployScript(dir, stage, port)
	if err := s.Append(ctx, "cat > "+Quote(path.Join(stage, "commit.sh")), strings.NewReader(script)); err != nil {
		return err
	}
	if _, err := s.Shell(ctx, "nohup sh "+Quote(path.Join(stage, "commit.sh"))+" > "+Quote(path.Join(stage, "deploy.log"))+" 2>&1 < /dev/null &"); err != nil {
		return err
	}
	note("手机正在安装并检查服务；此阶段即使连接中断，手机仍会完成切换或回滚")
	for i := 0; i < 60; i++ {
		select {
		case <-ctx.Done():
			return errors.New("连接已停止等待，手机安装仍可能继续；重新检测设备查看结果")
		case <-time.After(time.Second):
		}
		out, err := s.Shell(ctx, "if test -f "+Quote(path.Join(stage, "result"))+"; then cat "+Quote(path.Join(stage, "result"))+"; else echo pending; fi")
		if err != nil {
			return fmt.Errorf("无法读取手机安装结果；请重新检测设备，勿重复部署：%w", err)
		}
		result := strings.TrimSpace(string(out))
		if result == "ok" {
			note("手机服务已启动，旧程序保留在 .previous；账号和资源库配置保持原样")
			return nil
		}
		if result != "pending" {
			return fmt.Errorf("手机安装未完成：%s（日志：%s）", result, path.Join(stage, "deploy.log"))
		}
	}
	return fmt.Errorf("手机安装结果等待超时；请检查 %s", path.Join(stage, "deploy.log"))
}
func deployScript(dir, stage string, port int) string {
	result := Quote(path.Join(stage, "result"))
	return `set -eu
cd ` + Quote(dir) + `
lock=.omnihub-deploy-lock
if ! mkdir "$lock" 2>/dev/null; then
 p=$(cat "$lock/pid" 2>/dev/null || true)
 case "$p" in ''|*[!0-9]*) echo '部署锁不完整，请检查上次部署日志' > ` + result + `; exit 1;; esac
 if kill -0 "$p" 2>/dev/null; then echo '已有部署正在执行' > ` + result + `; exit 1; fi
 rm -f "$lock/pid"; rmdir "$lock"; mkdir "$lock"
fi
printf '%s' "$$" > "$lock/pid"
old=0; backup=0; changed=0
finish() {
 code=$?
 trap - EXIT
 if test "$code" != 0 && test ! -f ` + result + `; then
  if test "$changed" = 1; then
   ./omnihub.sh stop || true
   if test "$backup" = 1; then
    cp -p .previous/omnihub-server omnihub-server || true
    cp -p .previous/omnihub.sh omnihub.sh || true
    ./omnihub.sh start || true
   fi
  fi
  echo '安装失败，已尝试恢复旧版本；请查看 deploy.log' > ` + result + `
 fi
 rm -f "$lock/pid"; rmdir "$lock"
 exit "$code"
}
trap finish EXIT
for f in omnihub-server omnihub.sh omnihub-server.new omnihub.sh.new .previous .previous/omnihub-server .previous/omnihub.sh; do
 if test -L "$f"; then echo '程序或备份是符号链接，未更新' > ` + result + `; exit 1; fi
done
if test -f omnihub-server; then
 old=1
 test -f omnihub.sh
 mkdir -p .previous
 cp -p omnihub-server .previous/omnihub-server
 cp -p omnihub.sh .previous/omnihub.sh
 backup=1
 ./omnihub.sh stop
 # Verify that the old executable really exited, including launchers with a stale/missing PID file.
 for exe in /proc/[0-9]*/exe; do
  if test "$(readlink "$exe" 2>/dev/null || true)" = "$(pwd)/omnihub-server"; then
   echo '旧服务未停止，未替换程序' > ` + result + `; exit 1
  fi
 done
fi
changed=1
cp ` + Quote(path.Join(stage, "omnihub-server")) + ` omnihub-server.new
cp ` + Quote(path.Join(stage, "omnihub.sh")) + ` omnihub.sh.new
chmod 700 omnihub-server.new omnihub.sh.new
mv omnihub-server.new omnihub-server
mv omnihub.sh.new omnihub.sh
./omnihub.sh start
sleep 3
./omnihub.sh status
http=$(curl --silent --show-error --max-time 10 -o ` + Quote(path.Join(stage, "health.json")) + ` -w '%{http_code}' ` + Quote(fmt.Sprintf("http://127.0.0.1:%d/api/health", port)) + `)
if test "$http" != 200; then
 if ! { test "$old" = 0 && test "$http" = 503 && grep -q 'admin setup required' ` + Quote(path.Join(stage, "health.json")) + `; }; then exit 1; fi
fi
rm -f ` + Quote(path.Join(stage, "omnihub-server")) + ` ` + Quote(path.Join(stage, "omnihub.sh")) + `
echo ok > ` + result + `
`
}
