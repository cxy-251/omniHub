package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"image/png"
	"net/http/httptest"
	"omnihub/internal/auth"
	"omnihub/internal/library"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Original fixtures contain only our test instructions/colors, not game assets.
func testNES() []byte {
	rom := make([]byte, 16+16384+8192)
	copy(rom, []byte{'N', 'E', 'S', 26, 1, 1})
	code := []byte{0x78, 0xd8, 0xa2, 0xff, 0x9a, 0xa9, 0, 0x8d, 0, 0x20, 0x8d, 1, 0x20, 0x2c, 2, 0x20, 0x10, 0xfb, 0xa9, 8, 0x8d, 1, 0x20,
		0x2c, 2, 0x20, 0x10, 0xfb, 0xe6, 0, 0xa9, 0x3f, 0x8d, 6, 0x20, 0xa9, 0, 0x8d, 6, 0x20, 0xa5, 0, 0x29, 0x20, 0xf0, 4, 0xa9, 6, 0xd0, 2, 0xa9, 0x21, 0x8d, 7, 0x20, 0x4c, 0x17, 0x80}
	copy(rom[16:], code)
	for _, offset := range []int{0x3ffa, 0x3ffc, 0x3ffe} {
		binary.LittleEndian.PutUint16(rom[16+offset:], 0x8000)
	}
	return rom
}
func testSWF() []byte {
	bits := make([]byte, 0, 64)
	put := func(value, width int) {
		for shift := width - 1; shift >= 0; shift-- {
			bits = append(bits, byte((value>>shift)&1))
		}
	}
	put(14, 5)
	for _, value := range []int{0, 6400, 0, 4800} {
		put(value, 14)
	}
	rect := make([]byte, (len(bits)+7)/8)
	for i, bit := range bits {
		rect[i/8] |= bit << uint(7-i%8)
	}
	body := append(rect, 0, 12, 2, 0)
	for _, color := range [][]byte{{255, 0, 0}, {0, 0, 255}} {
		body = append(body, 0x43, 0x02)
		body = append(body, color...)
		body = append(body, 0x40, 0)
	}
	body = append(body, 0, 0)
	result := append([]byte{'F', 'W', 'S', 6, 0, 0, 0, 0}, body...)
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(result)))
	return result
}

func TestOfflineWebRuntimesInChrome(t *testing.T) {
	script := os.Getenv("OMNIHUB_WEB_RUNTIME_BROWSER_CHECK")
	if script == "" {
		t.Skip("optional actual Chrome runtime check")
	}
	state := t.TempDir()
	users, err := auth.OpenDB(filepath.Join(state, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := users.Close(); err != nil {
			t.Error(err)
		}
	})
	owner, err := users.CreateInitialAdmin("owner", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := users.CreateSession(owner.ID, "runtime-cookie", time.Hour); err != nil {
		t.Fatal(err)
	}
	manager := library.NewManager()
	root := t.TempDir()
	if _, err := manager.AddLibrary(root, false); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		folder, name string
		data         []byte
	}{{"retro_games/001 - NES", "test.nes", testNES()}, {"flash_games/002 - Flash", "test.swf", testSWF()}} {
		folder := filepath.Join(root, "standalone_games", filepath.FromSlash(fixture.folder))
		if err := os.Mkdir(folder, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, fixture.name), fixture.data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.SetPrimary(""); err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(manager, users, state, "native-token")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := router.Close(); err != nil {
			t.Error(err)
		}
	})
	server := httptest.NewServer(router)
	defer server.Close()
	payload, err := json.Marshal(map[string]string{"url": server.URL, "cookie": "runtime-cookie"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", script)
	command.Stdin = bytes.NewReader(payload)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("actual runtime check: %v\n%s", err, output)
	}
	if !bytes.Contains(output, []byte("WEB_RUNTIME_CHECK_OK")) {
		t.Fatalf("runtime completion unproven: %s", output)
	}
	for _, id := range []string{"001", "002"} {
		file, err := os.Open(filepath.Join(filepath.Dir(script), "..", "..", ".test-tmp", "web-runtime", id+".png"))
		if err != nil {
			t.Fatal(err)
		}
		frame, decodeErr := png.Decode(file)
		closeErr := file.Close()
		if decodeErr != nil || closeErr != nil {
			t.Fatalf("runtime pixels: %v %v", decodeErr, closeErr)
		}
		r, g, b, _ := frame.At(640, 450).RGBA()
		if id == "002" && (r < 60000 || g > 1000 || b > 1000) {
			t.Fatalf("SWF background pixels missing: %d %d %d", r, g, b)
		}
		if id == "001" && (r < 20000 && b < 20000) {
			t.Fatalf("NES pixels missing: %d %d %d", r, g, b)
		}
	}
	t.Logf("actual runtimes: %s", output)
}
