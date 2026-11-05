// Package phoneops handles explicit desktop-initiated Android operations.
package phoneops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"

	"omnihub/internal/platform"
)

// Remote streams file data independently from its small command responses.
type Remote interface {
	Shell(context.Context, string) ([]byte, error)
	Append(context.Context, string, io.Reader) error
}

type ADB struct{ Path, Serial string }
type Device struct {
	Serial   string `json:"serial"`
	Identity string `json:"identity"`
	Model    string `json:"model"`
	State    string `json:"state"`
	USB      bool   `json:"usb"`
}

// Quote escapes a single shell argument; command arguments must never be interpolated raw.
func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
func Token(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:16]) }

type cappedOutput struct{ bytes.Buffer }

func (b *cappedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 1<<20 {
		return 0, errors.New("命令输出超过限制")
	}
	return b.Buffer.Write(p)
}

func command(ctx context.Context, program string, args []string, input io.Reader) ([]byte, error) {
	if program == "" {
		return nil, errors.New("未找到所需工具")
	}
	cmd := platform.BackgroundCommand(ctx, program, args...)
	cmd.Stdin = input
	var out, errout cappedOutput
	cmd.Stdout = &out
	cmd.Stderr = &errout
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w：%s", err, strings.TrimSpace(errout.String()))
	}
	return bytes.ReplaceAll(out.Bytes(), []byte("\r\n"), []byte("\n")), nil
}
func (a ADB) Run(ctx context.Context, args ...string) ([]byte, error) {
	if a.Serial != "" {
		args = append([]string{"-s", a.Serial}, args...)
	}
	return command(ctx, a.Path, args, nil)
}
func (a ADB) Shell(ctx context.Context, script string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	return a.Run(ctx, "shell", "-T", script)
}
func (a ADB) Append(ctx context.Context, script string, r io.Reader) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	_, err := command(ctx, a.Path, []string{"-s", a.Serial, "shell", "-T", script}, r)
	return err
}
func (a ADB) Identity(ctx context.Context) (string, error) {
	out, err := a.Shell(ctx, "getprop ro.serialno")
	id := strings.TrimSpace(string(out))
	if err != nil {
		return "", err
	}
	if id == "" || id == "unknown" {
		return "", errors.New("无法读取手机唯一标识，拒绝续传到不确定的设备")
	}
	return id, nil
}

// Devices preserves unauthorized/offline entries so the UI can explain how to connect.
func Devices(ctx context.Context, adb string) ([]Device, error) {
	out, err := (ADB{Path: adb}).Run(ctx, "devices", "-l")
	if err != nil {
		return nil, err
	}
	result := []Device{}
	seen := map[string]int{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || f[0] == "List" || strings.HasPrefix(f[0], "*") {
			continue
		}
		d := Device{Serial: f[0], State: f[1], Model: f[0]}
		for _, v := range f[2:] {
			if strings.HasPrefix(v, "model:") {
				d.Model = strings.TrimPrefix(v, "model:")
			}
			if strings.HasPrefix(v, "usb:") {
				d.USB = true
			}
		}
		if d.State == "device" {
			transport := ADB{Path: adb, Serial: d.Serial}
			d.Identity, _ = transport.Identity(ctx)
			d.USB = transport.RequireUSB(ctx) == nil
		}
		if d.Identity != "" {
			if i, ok := seen[d.Identity]; ok {
				if d.USB && !result[i].USB {
					result[i] = d
				}
				continue
			}
			seen[d.Identity] = len(result)
		}
		result = append(result, d)
	}
	return result, nil
}

func validAbsolute(p string) bool {
	return strings.HasPrefix(p, "/") && p != "/" && path.Clean(p) == p && !strings.ContainsAny(p, "\x00\n\r\t")
}

// LibraryRoot accepts only an existing omniHub library, not an arbitrary phone directory.
func LibraryRoot(ctx context.Context, r Remote, root string) (string, error) {
	if !validAbsolute(root) {
		return "", errors.New("手机资源库路径必须是规范的绝对路径")
	}
	out, err := r.Shell(ctx, "test -f "+Quote(path.Join(root, "omnilibrary.json"))+" && cd "+Quote(root)+" && pwd -P")
	if err != nil {
		return "", fmt.Errorf("手机资源库不存在，请先在手机 omniHub 设置中添加资源库：%w", err)
	}
	canonical := strings.TrimSpace(string(out))
	if !validAbsolute(canonical) {
		return "", errors.New("无法确定手机资源库路径")
	}
	return canonical, nil
}
func Free(ctx context.Context, r Remote, dir string) (int64, error) {
	out, err := r.Shell(ctx, "stat -f -c '%a %S' "+Quote(dir))
	if err != nil {
		return 0, err
	}
	var blocks, size int64
	if _, err = fmt.Sscan(string(out), &blocks, &size); err != nil || blocks < 0 || size <= 0 || blocks > (1<<62)/size {
		return 0, errors.New("无法读取手机剩余空间")
	}
	return blocks * size, nil
}

// SafeDirs rejects symlinks in every component below the already canonical root.
func SafeDirs(root, rel string, create bool) string {
	s := "test -d " + Quote(root) + " && test ! -L " + Quote(root) + " || exit 40\n"
	p := root
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." {
			continue
		}
		p = path.Join(p, part)
		s += "test ! -L " + Quote(p) + " || exit 40\n"
		if create {
			s += "mkdir -p " + Quote(p) + " || exit 41\n"
		}
		s += "if test -e " + Quote(p) + "; then test -d " + Quote(p) + " || exit 40; fi\n"
	}
	return s
}
func RemoteSize(ctx context.Context, r Remote, p string) (int64, error) {
	out, err := r.Shell(ctx, "test ! -L "+Quote(p)+" || exit 40; if test -e "+Quote(p)+"; then test -f "+Quote(p)+" && stat -c %s "+Quote(p)+"; else echo -1; fi")
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
}
func RemoteHash(ctx context.Context, r Remote, p string) (string, error) {
	out, err := r.Shell(ctx, "test -f "+Quote(p)+" && test ! -L "+Quote(p)+" && sha256sum "+Quote(p))
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 || len(fields[0]) != 64 {
		return "", errors.New("手机未返回完整 SHA-256")
	}
	return fields[0], nil
}

// RequireUSB checks the transport reported by adbd, not the serial's appearance.
// A wireless connection to the same physical phone is deliberately insufficient.
func (a ADB) RequireUSB(ctx context.Context) error {
	c, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := a.Run(c, "get-devpath")
	if err != nil || !strings.HasPrefix(strings.TrimSpace(string(out)), "usb:") {
		return errors.New("漫画迁移需要 USB 数据线：请插线并授权 USB 调试；无线 ADB 可以检测设备和更新 omniHub 程序")
	}
	return nil
}

type WiredADB struct{ ADB }

func (a WiredADB) Shell(ctx context.Context, script string) ([]byte, error) {
	if err := a.RequireUSB(ctx); err != nil {
		return nil, err
	}
	return a.ADB.Shell(ctx, script)
}
func (a WiredADB) Append(ctx context.Context, script string, in io.Reader) error {
	if err := a.RequireUSB(ctx); err != nil {
		return err
	}
	return a.ADB.Append(ctx, script, in)
}
