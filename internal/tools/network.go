package tools

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
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
