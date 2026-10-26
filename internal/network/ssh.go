package network

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SSHStatus 是 Termux 里 SSH 服务的情况（手机上用来从电脑更新 omniHub，开机脚本会启动它）。
// 它不归「只在家里的网络上对外服务」管，在外面的 Wi-Fi 上也开着，所以设置页要让人看到它是否只允许密钥登录。
type SSHStatus struct {
	Running bool `json:"running"`  // 8022 端口在监听
	KeyOnly bool `json:"key_only"` // 密码登录和键盘交互登录都关掉了
}

// TermuxSSH 读取 Termux 的 SSH 配置；不在 Termux 里（没有 $PREFIX/etc/ssh）时返回 nil。
func TermuxSSH() *SSHStatus {
	prefix := os.Getenv("PREFIX")
	if prefix == "" || !strings.Contains(prefix, "com.termux") {
		return nil
	}
	main := filepath.Join(prefix, "etc", "ssh", "sshd_config")
	if _, err := os.Stat(main); err != nil {
		return nil
	}
	st := &SSHStatus{}
	if c, err := net.DialTimeout("tcp", "127.0.0.1:8022", time.Second); err == nil {
		c.Close()
		st.Running = true
	}
	// sshd 对同一个选项取第一次出现的值；sshd_config 开头 Include 了 sshd_config.d/*.conf，所以先读它们
	files, _ := filepath.Glob(filepath.Join(prefix, "etc", "ssh", "sshd_config.d", "*.conf"))
	sort.Strings(files)
	values := map[string]string{}
	for _, f := range append(files, main) {
		readSSHOptions(f, values)
	}
	// 键盘交互登录在 OpenSSH 里默认是开的
	st.KeyOnly = values["passwordauthentication"] == "no" && values["kbdinteractiveauthentication"] == "no"
	return st
}

func readSSHOptions(path string, values map[string]string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) > 0 && strings.EqualFold(fields[0], "Match") {
			return // Match 之后的选项只对部分连接生效，不算全局设置
		}
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		key := strings.ToLower(fields[0])
		if _, ok := values[key]; !ok {
			values[key] = strings.ToLower(fields[1])
		}
	}
}
