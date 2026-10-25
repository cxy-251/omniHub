package shortvideo

import (
	"context"
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
		Dir: dir,
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

	plan, l, err := PlanPhone(context.Background(), src, phone)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Count != 4 || len(plan.Back) != 2 || len(plan.Archives) != 3 { // 推：a（合并后多了 2）、n（手机没有）、o（顺序不同）
		t.Fatalf("plan = %+v", plan)
	}
	res, err := RunPhone(context.Background(), src, phone, plan, l, nil)
	if err != nil || res.Copied != 4 || res.Back != 2 || res.Archives != 3 {
		t.Fatalf("res = %+v, %v", res, err)
	}
	for rel, want := range map[string]string{"抖音/a/it's 2.mp4": "two", "抖音/a/album/01.webp": "img", "抖音/n/1.mp4": "new creator"} {
		if got := read(dst, rel); got != want {
			t.Fatalf("phone %s = %q", rel, got)
		}
	}
	for _, rel := range []string{"抖音/a/downloaded.txt", "抖音/n/downloaded.txt", "抖音/o/downloaded.txt", "快手/old/downloaded.txt"} {
		if read(src, rel) == "" || read(src, rel) != read(dst, rel) { // 逐字节一致
			t.Fatalf("%s: src %q phone %q", rel, read(src, rel), read(dst, rel))
		}
	}
	if got := read(src, "抖音/a/downloaded.txt"); got != "douyin 1\ndouyin 2\ndouyin 9\n" {
		t.Fatalf("src record = %q", got)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dst), ".omnihub-incoming")); err == nil {
		t.Fatal("incoming left behind")
	}
	again, l, err := PlanPhone(context.Background(), src, phone)
	if err != nil || again.Count != 0 || len(again.Back) != 0 || len(again.Archives) != 0 {
		t.Fatalf("again = %+v, %v", again, err)
	}

	clean, err := PlanClean(src, l, []string{sd})
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
