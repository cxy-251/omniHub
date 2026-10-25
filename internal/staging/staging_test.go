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
	put("staging/b1/media_library/shortvideo/抖音/a/"+RecordFile, "douyin 2\ndouyin 3\n")
	put("staging/b1/media_library/shortvideo/抖音/n/"+RecordFile, "douyin 7\n")
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
