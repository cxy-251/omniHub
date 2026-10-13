package tools

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
)

// SendWakeOnLAN 向指定 MAC 地址广播发送标准 Wake-on-LAN 魔术包（第 11.3.3 节）。
func SendWakeOnLAN(macAddr string) error {
	cleanMAC := strings.ReplaceAll(macAddr, ":", "")
	cleanMAC = strings.ReplaceAll(cleanMAC, "-", "")
	if len(cleanMAC) != 12 {
		return errors.New("invalid MAC address format")
	}

	macBytes, err := hex.DecodeString(cleanMAC)
	if err != nil {
		return fmt.Errorf("failed to decode MAC bytes: %w", err)
	}

	// 构造 102 字节魔术包：6 个 0xFF 加上重复 16 次的 6 字节 MAC 地址
	magicPacket := make([]byte, 102)
	for i := 0; i < 6; i++ {
		magicPacket[i] = 0xFF
	}
	for i := 0; i < 16; i++ {
		copy(magicPacket[6+i*6:], macBytes)
	}

	// 局域网广播地址 (255.255.255.255:9)
	bcastAddr, err := net.ResolveUDPAddr("udp", "255.255.255.255:9")
	if err != nil {
		return err
	}

	conn, err := net.DialUDP("udp", nil, bcastAddr)
	if err != nil {
		return fmt.Errorf("failed to dial UDP broadcast: %w", err)
	}
	defer conn.Close()

	if _, err := conn.Write(magicPacket); err != nil {
		return fmt.Errorf("failed to send magic packet: %w", err)
	}
	return nil
}

// LANDevice 记录局域网活跃设备信息（第 11.3.1 节）。
type LANDevice struct {
	IP       string `json:"ip"`
	MAC      string `json:"mac"`
	Hostname string `json:"hostname"`
	Device   string `json:"device"`
}

// ScanLAN 扫描读取局域网内活跃设备（第 11.3.1 节）。
func ScanLAN() ([]LANDevice, error) {
	data, err := os.ReadFile("/proc/net/arp")
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc/net/arp: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	var devices []LANDevice
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue // 跳过表头和空行
		}
		fields := strings.Fields(line)
		if len(fields) >= 6 {
			ip := fields[0]
			mac := fields[3]
			dev := fields[5]

			if mac == "00:00:00:00:00:00" || mac == "" {
				continue
			}

			hostname := ""
			names, _ := net.LookupAddr(ip)
			if len(names) > 0 {
				hostname = strings.TrimSuffix(names[0], ".")
			}

			devices = append(devices, LANDevice{
				IP:       ip,
				MAC:      mac,
				Hostname: hostname,
				Device:   dev,
			})
		}
	}
	return devices, nil
}
