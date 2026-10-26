package staging

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// 插件的已下载名单并进库里时和已有的合并，不能覆盖掉库里多出的记录。
func TestImportMergesRecords(t *testing.T) {
	root := t.TempDir()
	put := func(rel, data string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("media_library/shortvideo/抖音/a/"+RecordFile, "douyin 1\ndouyin 2\n")
	put(Dir+"/b1/media_library/shortvideo/抖音/a/"+RecordFile, "douyin 2\ndouyin 3\n")
	put(Dir+"/b1/media_library/shortvideo/抖音/n/"+RecordFile, "douyin 7\n")
	if _, err := Import(context.Background(), root, "b1", nil, nil); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]string{"a": "douyin 1\ndouyin 2\ndouyin 3\n", "n": "douyin 7\n"} {
		b, _ := os.ReadFile(filepath.Join(root, "media_library/shortvideo/抖音", rel, RecordFile))
		if string(b) != want {
			t.Fatalf("%s = %q, want %q", rel, b, want)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "staging/b1")); err == nil {
		t.Fatal("batch left behind")
	}
}

// 旧位置（库根目录的 staging/）里 omniHub 建的批次搬到 media_library/staging/，别的东西不动。
func TestMigrateLegacyStaging(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"staging/b1/" + manifest, "staging/b1/gallery/a.jpg", "staging/other/x.log", "staging/pack.log"} {
		p := filepath.Join(root, filepath.FromSlash(f))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	batches, err := List(root)
	if err != nil || len(batches) != 1 || batches[0].Name != "b1" {
		t.Fatalf("batches = %+v, %v", batches, err)
	}
	for f, want := range map[string]bool{Dir + "/b1/gallery/a.jpg": true, "staging/b1": false, "staging/other/x.log": true, "staging/pack.log": true} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(f))); (err == nil) != want {
			t.Errorf("%s exists = %v, want %v", f, err == nil, want)
		}
	}
}
