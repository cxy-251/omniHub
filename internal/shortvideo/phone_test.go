package shortvideo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// fakePhone 用本机的 sh 和 cp 代替 adb shell 与 adb push，手机目录是一个临时目录。
func fakePhone(t *testing.T) (Phone, string) {
	root := t.TempDir()
	dir := filepath.Join(root, "omni_library/media_library/shortvideo")
	os.MkdirAll(dir, 0o755)
	return Phone{
		Dir:     dir,
		Reserve: 1 << 20,
		Shell: func(ctx context.Context, script string) ([]byte, error) {
			return exec.CommandContext(ctx, "sh", "-c", script).Output()
		},
		Push: func(ctx context.Context, files []string, remoteDir string) error {
			return exec.CommandContext(ctx, "cp", append(append([]string{"-r"}, files...), remoteDir+"/")...).Run()
		},
	}, dir
}

func TestPhoneSyncAndClean(t *testing.T) {
	src, sd := t.TempDir(), t.TempDir()
	phone, dst := fakePhone(t)
	put := func(root, rel, data string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func(root, rel string) string {
		b, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		return string(b)
	}
	put(src, "抖音/a/1.mp4", "one")
	put(src, "抖音/a/it's 2.mp4", "two")
	put(src, "抖音/a/album/01.webp", "img")
	put(src, "抖音/a/downloaded.txt", "douyin 1\ndouyin 2\n")
	put(src, "抖音/n/1.mp4", "new creator")
	put(src, "抖音/n/downloaded.txt", "douyin 5\n")
	put(src, "抖音/s/1.mp4", "sd creator")
	put(dst, "抖音/a/1.mp4", "one")
	put(dst, "抖音/a/downloaded.txt", "douyin 9\ndouyin 1\n")
	put(dst, "快手/old/downloaded.txt", "kuaishou 3\n")
	put(src, "抖音/o/downloaded.txt", "douyin 7\ndouyin 8\n") // 两边记录相同、顺序不同
	put(dst, "抖音/o/downloaded.txt", "douyin 8\ndouyin 7\n")
	put(sd, "抖音/s/other.mp4", "x") // SD 卡有这个博主，但还没补 1.mp4

	plan, l, err := PlanPhone(context.Background(), src, phone, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Count != 4 {
		t.Fatalf("plan = %+v", plan)
	}
	res, err := RunPhone(context.Background(), src, phone, plan, l, nil)
	if err != nil || res.Copied != 4 {
		t.Fatalf("res = %+v, %v", res, err)
	}
	for rel, want := range map[string]string{"抖音/a/it's 2.mp4": "two", "抖音/a/album/01.webp": "img", "抖音/n/1.mp4": "new creator"} {
		if got := read(dst, rel); got != want {
			t.Fatalf("phone %s = %q", rel, got)
		}
	}
	// 下载记录两边都不动
	if read(src, "抖音/a/downloaded.txt") != "douyin 1\ndouyin 2\n" || read(dst, "抖音/a/downloaded.txt") != "douyin 9\ndouyin 1\n" || read(dst, "抖音/n/downloaded.txt") != "" {
		t.Fatal("records touched")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dst), ".omnihub-incoming")); err == nil {
		t.Fatal("incoming left behind")
	}
	again, l, err := PlanPhone(context.Background(), src, phone, nil)
	if err != nil || again.Count != 0 {
		t.Fatalf("again = %+v, %v", again, err)
	}

	clean, err := PlanClean(src, l, []string{sd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if clean.Files != 4 || clean.NotOnKeep != 1 || len(clean.Items) != 4 { // 1.mp4、it's 2.mp4、album 目录、n/1.mp4；s/1.mp4 等 SD 卡
		t.Fatalf("clean = %+v %v", clean, clean.Items)
	}
	var trashed []string
	if _, err := RunClean(context.Background(), clean, func(p string) error { trashed = append(trashed, p); return os.RemoveAll(p) }, nil); err != nil {
		t.Fatal(err)
	}
	if read(src, "抖音/a/downloaded.txt") == "" || read(src, "抖音/s/1.mp4") == "" {
		t.Fatal("clean removed records or unsynced video")
	}
}

func TestPhoneStopsWhenFull(t *testing.T) {
	src := t.TempDir()
	phone, dst := fakePhone(t)
	os.MkdirAll(filepath.Join(src, "抖音/a"), 0o755)
	os.WriteFile(filepath.Join(src, "抖音/a/1.mp4"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(src, "抖音/a/downloaded.txt"), []byte("douyin 1\n"), 0o644)
	plan, l, err := PlanPhone(context.Background(), src, phone, nil)
	if err != nil || plan.PhoneFree <= 0 {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	plan.Files[0].Size = plan.PhoneFree // 假装这个文件大到放不下
	res, err := RunPhone(context.Background(), src, phone, plan, l, nil)
	if !errors.Is(err, ErrPhoneFull) || res.Copied != 0 {
		t.Fatalf("res = %+v, %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "抖音/a/1.mp4")); err == nil {
		t.Fatal("copied despite full phone")
	}
}

func TestPhoneResumesAfterDisconnect(t *testing.T) {
	src := t.TempDir()
	phone, dst := fakePhone(t)
	for i, name := range []string{"1.mp4", "2.mp4"} {
		p := filepath.Join(src, "抖音/a", name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte{byte(i)}, 0o644)
	}
	os.MkdirAll(filepath.Join(src, "MV/x"), 0o755)
	os.WriteFile(filepath.Join(src, "MV/x/song.mp4"), []byte("mv"), 0o644)
	push, fails, reconnects := phone.Push, 1, 0
	phone.Push = func(ctx context.Context, files []string, dir string) error {
		if fails > 0 { // 第一次推送中途断线：只传了一半
			fails--
			os.WriteFile(filepath.Join(dir, filepath.Base(files[0])), nil, 0o644)
			return errors.New("device offline")
		}
		return push(ctx, files, dir)
	}
	phone.Reconnect = func(context.Context) error { reconnects++; return nil }
	platforms := []string{"抖音", "快手", "TikTok", "小红书"}
	plan, l, err := PlanPhone(context.Background(), src, phone, platforms)
	if err != nil || plan.Count != 2 {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	res, err := RunPhone(context.Background(), src, phone, plan, l, nil)
	if err != nil || res.Copied != 2 || reconnects != 1 {
		t.Fatalf("res = %+v, reconnects %d, %v", res, reconnects, err)
	}
	for i, name := range []string{"1.mp4", "2.mp4"} {
		if b, _ := os.ReadFile(filepath.Join(dst, "抖音/a", name)); len(b) != 1 || b[0] != byte(i) {
			t.Fatalf("%s = %v", name, b)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "MV")); err == nil {
		t.Fatal("MV synced")
	}
	os.MkdirAll(filepath.Join(dst, "MV/x"), 0o755) // 手机上也有这首 MV，清理时也不能动它
	os.WriteFile(filepath.Join(dst, "MV/x/song.mp4"), []byte("mv"), 0o644)
	after, err := phone.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	clean, _ := PlanClean(src, after, nil, platforms)
	if clean.Files != 2 {
		t.Fatalf("clean = %+v", clean)
	}
	for _, item := range clean.Items {
		if filepath.Base(filepath.Dir(filepath.Dir(item))) == "MV" {
			t.Fatal("MV cleaned")
		}
	}
}
