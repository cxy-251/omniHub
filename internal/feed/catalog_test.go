package feed

import "testing"

// 短视频每条有 6 个标签，最后一个（编码）以前被丢掉，网页端按编码筛选时视频全没了。
func TestCatalogKeepsShortVideoTags(t *testing.T) {
	var c Catalog
	tags := []string{"creator:抖音/a", "platform:抖音", "kind:video", "perm:shortvideo/抖音", "dur:short", "codec:h264"}
	c.Add("抖音/a/1.mp4", "lib", "抖音/a", tags, 0, 0)
	if id := c.Lookup("codec:h264"); id < 0 || !c.HasTag(&c.Items[0], id) {
		t.Fatalf("codec tag dropped: %v", c.TagNames(&c.Items[0]))
	}
}
