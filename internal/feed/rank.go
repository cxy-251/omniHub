// Package feed 是短视频、相册等内容共用的推荐排序（纯计算，不读写存储）。
//
// 每个条目带若干特征标签（如 creator:抖音/某人、platform:抖音、color:3、shape:portrait）。
// 账号的偏好是「标签 → 权重」，由点赞、看完、快速划走、移到回收站等行为累积，按半衰期衰减（见 userstate）。
// 排序：按偏好、新鲜度、是否看过、是否固定的分组算出每个条目的权重，用种子做加权随机抽样
// （Efraimidis–Spirakis：key = u^(1/w)），同一个种子顺序稳定，换种子就换一批；最后打散，避免同组连着出现。
package feed

import (
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"time"
)

// Item 是一个候选条目。
type Item struct {
	ID    string
	Lib   string   // 所在资源库 ID
	Group string   // 打散用的分组（短视频是博主，相册是色系），也是「固定」的对象
	Tags  []string // 特征标签
	Date  int64    // 发布/拍摄时间（Unix 秒，0 表示未知）
	Ratio int      // 高宽比 ×1000（相册瀑布流用，其它为 0）
}

// Seen 是条目被看过的记录。
type Seen struct {
	Count int
	Last  int64 // Unix 秒
}

// Signals 是排序时用到的账号状态。
type Signals struct {
	Profile map[string]float64 // 标签权重（已衰减到当前）
	Liked   map[string]bool
	Seen    map[string]Seen
	Pinned  map[string]bool // 固定的分组
}

// TagWeight 是各类标签对推荐的影响力：具体的（博主、色调）高，宽泛的（平台、类型、明暗）低，
// 否则人人都有的宽泛标签会把所有条目一起抬高，拉不开差距。
var TagWeight = map[string]float64{
	"creator": 2.0, "platform": 0.25, "kind": 0.2, "dur": 0.3,
	"color": 0.8, "hue": 0.8, "light": 0.3, "shape": 0.4,
}

func tagWeight(tag string) float64 {
	if i := strings.IndexByte(tag, ':'); i > 0 {
		if w, ok := TagWeight[tag[:i]]; ok {
			return w
		}
	}
	return 0.5
}

// Weight 返回条目被抽中的相对权重（>0）。
func Weight(it *Item, s Signals, now time.Time) float64 {
	score := 0.0
	for _, t := range it.Tags {
		// 每个标签单独压到 (-1, 1)：一个标签再强也有上限，保留探索。
		if p := s.Profile[t]; p != 0 {
			score += tagWeight(t) * math.Tanh(p/3)
		}
	}
	w := math.Exp(score)
	if it.Date > 0 {
		age := now.Sub(time.Unix(it.Date, 0)).Hours() / 24
		if age < 0 {
			age = 0
		}
		w *= 1 + math.Exp(-age/30) // 一个月内的新内容最多加倍
	}
	if seen, ok := s.Seen[it.ID]; ok {
		days := now.Sub(time.Unix(seen.Last, 0)).Hours() / 24
		switch {
		case days < 3:
			w *= 0.2
		case days < 14:
			w *= 0.5
		default:
			w *= 0.85
		}
	}
	if s.Pinned[it.Group] {
		w *= 1.8
	}
	if s.Liked[it.ID] {
		w *= 0.5 // 点过赞的偶尔再出现，但不霸占推荐
	}
	return w
}

func unit(seed int64, id string) float64 {
	h := fnv.New64a()
	var b [8]byte
	for i := range b {
		b[i] = byte(seed >> (8 * i))
	}
	_, _ = h.Write(b[:])
	_, _ = h.Write([]byte(id))
	// FNV 对相似的短 ID 混合不够（相近的 ID 得到相近的值），再过一遍 splitmix64 的终混。
	x := h.Sum64()
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	// (0,1) 之间
	return (float64(x>>11) + 0.5) / float64(1<<53)
}

// Rank 返回推荐顺序（items 的下标）。
func Rank(items []Item, s Signals, seed int64, now time.Time) []int {
	type keyed struct {
		idx int
		key float64
	}
	// 按分组大小开方归一：作品多的分组不会只因数量多就霸占推荐，小分组也有露面机会。
	sizes := map[string]int{}
	for i := range items {
		sizes[items[i].Group]++
	}
	list := make([]keyed, len(items))
	for i := range items {
		w := Weight(&items[i], s, now)
		if n := sizes[items[i].Group]; n > 1 {
			w /= math.Sqrt(float64(n))
		}
		// log(u)/w 与 u^(1/w) 同序，数值更稳定。
		list[i] = keyed{i, math.Log(unit(seed, items[i].ID)) / w}
	}
	sort.Slice(list, func(a, b int) bool { return list[a].key > list[b].key })
	order := make([]int, len(list))
	for i, k := range list {
		order[i] = k.idx
	}
	return Spread(items, order, 2, 24)
}

// Spread 打散：尽量让同一分组不出现在最近 gap 个位置里，最多往后找 look 个。顺序其余部分不变。
// 只在窗口内挪动，耗时 O(n·look)。
func Spread(items []Item, order []int, gap, look int) []int {
	out := append([]int(nil), order...)
	for i := range out {
		for j := i; j < len(out) && j < i+look; j++ {
			if !recent(items, out[:i], items[out[j]].Group, gap) {
				if j > i {
					picked := out[j]
					copy(out[i+1:j+1], out[i:j])
					out[i] = picked
				}
				break
			}
		}
	}
	return out
}

func recent(items []Item, out []int, group string, gap int) bool {
	if group == "" {
		return false
	}
	for k := len(out) - 1; k >= 0 && k >= len(out)-gap; k-- {
		if items[out[k]].Group == group {
			return true
		}
	}
	return false
}
