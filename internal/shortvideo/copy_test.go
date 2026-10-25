package shortvideo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPlanAndRunCopy(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	put := func(root, rel, data string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put(src, "抖音/a/1.mp4", "one")
	put(src, "抖音/a/2.mp4", "two")
	put(src, "抖音/a/album/01.webp", "img")
	put(src, "抖音/a/3.mp4", "src-version")
	put(src, "抖音/a/downloaded.txt", "douyin 1\ndouyin 2\n")
	put(src, "抖音/a/4.mp4.part", "half")
	put(src, "抖音/b/1.mp4", "other creator")
	put(src, "index.db", "db")
	put(src, "X/collection.json", "{}")
	put(dst, "抖音/a/1.mp4", "one")
	put(dst, "抖音/a/3.mp4", "dst")
	put(dst, "抖音/a/downloaded.txt", "douyin 9\ndouyin 1")

	plan, err := PlanCopy(src, dst, true)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Count != 2 || plan.Skipped != 1 || len(plan.Conflicts) != 1 || len(plan.Archives) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	if !plan.Keeps("抖音/a/album") || plan.Keeps("抖音/b/1.mp4") {
		t.Fatal("Keeps")
	}
	res, err := RunCopy(context.Background(), src, dst, plan, nil)
	if err != nil || res.Copied != 2 || res.Archives != 1 {
		t.Fatalf("res = %+v, %v", res, err)
	}
	read := func(rel string) string {
		b, _ := os.ReadFile(filepath.Join(dst, filepath.FromSlash(rel)))
		return string(b)
	}
	if read("抖音/a/album/01.webp") != "img" || read("抖音/a/3.mp4") != "dst" {
		t.Fatal("copy content")
	}
	if got := read("抖音/a/downloaded.txt"); got != "douyin 9\ndouyin 1\ndouyin 2\n" {
		t.Fatalf("archive = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dst, "抖音/b")); err == nil {
		t.Fatal("creator not on dst was copied")
	}
	if again, _ := PlanCopy(src, dst, true); again.Count != 0 || len(again.Archives) != 0 {
		t.Fatalf("second plan = %+v", again)
	}
	all, _ := PlanCopy(src, dst, false)
	if all.Count != 2 { // 抖音/b/1.mp4 与 X/collection.json
		t.Fatalf("all = %+v", all)
	}
}
