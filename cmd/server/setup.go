package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"omnihub/internal/config"
)

// setupAdmin 为没有桌面客户端的设备（安卓 Termux 等）创建第一个管理员：读取正在运行的服务写下的本机凭证
// （local-client.json，只有本机当前用户可读），从终端读入密码，调用服务的初始化接口。服务必须已在运行。
func setupAdmin(name string) error {
	dir, err := config.GetConfigDir()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(dir, "local-client.json"))
	if err != nil {
		return errors.New("没有找到正在运行的服务（先在另一个终端启动 omnihub-server）")
	}
	var local struct{ URL, Token string }
	if err := json.Unmarshal(data, &local); err != nil || local.URL == "" || local.Token == "" {
		return errors.New("本机凭证文件无效")
	}
	fmt.Fprintf(os.Stderr, "为 %s 设置密码（输入时会显示在屏幕上）：", name)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	password := strings.TrimRight(line, "\r\n")
	if password == "" {
		return errors.New("密码不能为空")
	}
	body, _ := json.Marshal(map[string]string{"username": name, "password": password})
	req, err := http.NewRequest(http.MethodPost, local.URL+"/api/auth/setup", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Omni-Local-Token", local.Token)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("连不上服务：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("服务返回 %d：%s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}
