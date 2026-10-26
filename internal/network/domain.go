package network

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// InstanceID 是这次运行的随机标识，经公网域名读回来时用它判断域名是否指向这台设备
// （几台设备都跑 omniHub 时，域名可能指向别的设备）。
var InstanceID = func() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}()

var hostnameRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// ValidHostname 检查是否是一个域名（不含协议、端口和路径）。
func ValidHostname(host string) bool { return len(host) <= 253 && hostnameRe.MatchString(host) }

// SetDomain 把隧道配置里第一条 ingress 规则的 hostname 换成 host（只改这一行，其余原样保留，旧文件存为 .bak），
// 然后在 Cloudflare 上把这个域名指向隧道（cloudflared tunnel route dns，需要登录过的 cert.pem），
// 隧道开着时重启它。返回 route dns 的输出。
func (t *Tunnel) SetDomain(ctx context.Context, host string) (string, error) {
	if !ValidHostname(host) {
		return "", errors.New("域名格式不对，例如 phone.example.com")
	}
	bin := t.Status().Binary
	if bin == "" {
		return "", errors.New("没有找到 cloudflared")
	}
	data, err := os.ReadFile(t.config)
	if err != nil {
		return "", fmt.Errorf("读不到隧道配置：%w", err)
	}
	lines := strings.Split(string(data), "\n")
	tunnel, replaced := "", false
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if v, ok := strings.CutPrefix(line, "tunnel:"); ok && tunnel == "" {
			tunnel = strings.Trim(strings.TrimSpace(v), `"'`)
		}
		body := strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if !replaced && strings.HasPrefix(body, "hostname:") {
			lines[i] = raw[:strings.Index(raw, "hostname:")] + "hostname: " + host
			replaced = true
		}
	}
	if tunnel == "" || !replaced {
		return "", errors.New("隧道配置里没有 tunnel 或 ingress 的 hostname，请先按 cloudflared 的说明建好命名隧道")
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, bin, "--config", t.config, "tunnel", "route", "dns", tunnel, host).CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if err != nil {
		return msg, fmt.Errorf("在 Cloudflare 上设置域名失败（配置没改）：%v %s", err, msg)
	}
	if err := os.WriteFile(t.config+".bak", data, 0o600); err != nil {
		return msg, err
	}
	if err := os.WriteFile(t.config, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		return msg, err
	}
	t.mu.Lock()
	running := t.cancel != nil
	t.mu.Unlock()
	if running {
		t.SetEnabled(false)
		t.SetEnabled(true)
	}
	return msg, nil
}

// DomainCheck 是经公网域名访问的结果。
type DomainCheck struct {
	Domain string `json:"domain"`
	OK     bool   `json:"ok"`              // 能访问，并且是这台设备
	Other  bool   `json:"other"`           // 能访问，但是另一台 omniHub
	Error  string `json:"error,omitempty"` // 访问不了的原因
}

// CheckDomain 经 https://host 读取 omniHub 的运行标识，判断域名是否通、是否指向这台设备。
func CheckDomain(ctx context.Context, host string) DomainCheck {
	res := DomainCheck{Domain: host}
	if !ValidHostname(host) {
		res.Error = "域名格式不对"
		return res
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(cctx, http.MethodGet, "https://"+host+"/api/network/identity", nil)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var id struct {
		ID string `json:"id"`
	}
	switch {
	case resp.StatusCode == http.StatusForbidden:
		res.Error = "隧道通了，但这台设备的广域网访问是关着的"
	case resp.StatusCode >= 520 && resp.StatusCode <= 530:
		res.Error = fmt.Sprintf("Cloudflare 连不上隧道（HTTP %d）：隧道没开，或域名指向的隧道不在线", resp.StatusCode)
	case resp.StatusCode != http.StatusOK || json.Unmarshal(body, &id) != nil || id.ID == "":
		res.Error = fmt.Sprintf("域名能访问，但不是 omniHub（HTTP %d）", resp.StatusCode)
	case id.ID == InstanceID:
		res.OK = true
	default:
		res.Other = true
		res.Error = "这个域名指向另一台设备上的 omniHub"
	}
	return res
}
