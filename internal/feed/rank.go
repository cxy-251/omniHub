// Package feed 是短视频、相册等内容共用的推荐排序（纯计算，不读写存储）。
//
// 每个条目带若干特征标签（如 creator:抖音/某人、platform:抖音、color:3、shape:portrait）。
// 账号的偏好是「标签 → 权重」，由点赞、看完、快速划走、移到回收站等行为累积，按半衰期衰减（见 userstate）。
// 排序：按偏好、新鲜度、是否看过、是否固定的分组算出每个条目的权重，用种子做加权随机抽样
// （Efraimidis–Spirakis：key = u^(1/w)），同一个种子顺序稳定，换种子就换一批；最后打散，避免同组连着出现。
//
// 内存：相册有 10 万条。标签与分组名放进 Catalog 的字典，条目里只存编号（不为每条分配字符串）。
package feed

import (
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"time"
)

// MaxTags 是每个条目最多的标签数。
const MaxTags = 5

// Item 是一个候选条目。Group 与 Tags 是 Catalog.Names 里的编号（-1 表示空）。
type Item struct {
	ID    string
	Lib   string // 所在资源库 ID（各条目共用同一个字符串）
	Group int32  // 打散用的分组（短视频是博主，相册是色系），也是「固定」的对象
	Tags  [MaxTags]int32
	Date  int64 // 发布/拍摄时间（Unix 秒，0 表示未知）
	Ratio int32 // 高宽比 ×1000（未知为 0）
}

// Catalog 是一类内容的全部条目与标签字典。
type Catalog struct {
	Items []Item
	Names []string
	ids   map[string]int32
}

// Intern 返回名字的编号（没有就加入字典）。
func (c *Catalog) Intern(name string) int32 {
	if c.ids == nil {
		c.ids = map[string]int32{}
	}
	if id, ok := c.ids[name]; ok {
		return id
	}
	id := int32(len(c.Names))
	c.Names = append(c.Names, name)
	c.ids[name] = id
	return id
}

// Lookup 返回名字的编号，字典里没有时为 -1。
func (c *Catalog) Lookup(name string) int32 {
	if id, ok := c.ids[name]; ok {
		return id
	}
	return -1
}

// Add 追加一个条目；tags 超过 MaxTags 的部分忽略。
func (c *Catalog) Add(id, lib, group string, tags []string, date int64, ratio int) {
	it := Item{ID: id, Lib: lib, Group: c.Intern(group), Date: date, Ratio: int32(ratio)}
	for k := range it.Tags {
		it.Tags[k] = -1
		if k < len(tags) && tags[k] != "" {
			it.Tags[k] = c.Intern(tags[k])
		}
	}
	c.Items = append(c.Items, it)
}

// GroupName 返回条目的分组名。
func (c *Catalog) GroupName(it *Item) string {
	if it.Group < 0 {
		return ""
	}
	return c.Names[it.Group]
}

// TagNames 返回条目的标签名（行为上报时更新偏好用）。
func (c *Catalog) TagNames(it *Item) []string {
	var out []string
	for _, t := range it.Tags {
		if t >= 0 {
			out = append(out, c.Names[t])
		}
	}
	return out
}

// HasTag 判断条目是否带某个标签（tag 为 Lookup 得到的编号）。
func (c *Catalog) HasTag(it *Item, tag int32) bool {
	if tag < 0 {
		return false
	}
	for _, t := range it.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// Find 按 ID 找条目下标（线性查找：10 万条约 0.2ms，省掉一张 ID 索引表），找不到为 -1。
func (c *Catalog) Find(id string) int {
	for i := range c.Items {
		if c.Items[i].ID == id {
			return i
		}
	}
	return -1
}

// Seen 是条目被看过的记录。
type Seen struct {
	Count int
	Last  int64 // Unix 秒
}

// Signals 是排序时用到的账号状态（以名字为键，排序时换成编号）。
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

// scorer 把按名字的账号状态换成按编号的数组，排序时不再查字符串。
type scorer struct {
	tagScore []float64 // 编号 → 该标签对分数的贡献
	pinned   []bool    // 分组编号 → 是否固定
	s        Signals
	now      time.Time
}

func newScorer(c *Catalog, s Signals, now time.Time) scorer {
	sc := scorer{tagScore: make([]float64, len(c.Names)), pinned: make([]bool, len(c.Names)), s: s, now: now}
	for name, p := range s.Profile {
		if id := c.Lookup(name); id >= 0 && p != 0 {
			// 每个标签单独压到 (-1, 1)：一个标签再强也有上限，保留探索。
			sc.tagScore[id] = tagWeight(name) * math.Tanh(p/3)
		}
	}
	for name := range s.Pinned {
		if id := c.Lookup(name); id >= 0 {
			sc.pinned[id] = true
		}
	}
	return sc
}

// weight 返回条目被抽中的相对权重（>0）。
func (sc *scorer) weight(it *Item) float64 {
	score := 0.0
	for _, t := range it.Tags {
		if t >= 0 {
			score += sc.tagScore[t]
		}
	}
	w := math.Exp(score)
	if it.Date > 0 {
		age := sc.now.Sub(time.Unix(it.Date, 0)).Hours() / 24
		if age < 0 {
			age = 0
		}
		w *= 1 + math.Exp(-age/30) // 一个月内的新内容最多加倍
	}
	if seen, ok := sc.s.Seen[it.ID]; ok {
		days := sc.now.Sub(time.Unix(seen.Last, 0)).Hours() / 24
		switch {
		case days < 3:
			w *= 0.2
		case days < 14:
			w *= 0.5
		default:
			w *= 0.85
		}
	}
	if it.Group >= 0 && sc.pinned[it.Group] {
		w *= 1.8
	}
	if sc.s.Liked[it.ID] {
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

// Rank 对 c.Items 里给定的下标排序，返回推荐顺序（仍是 c.Items 的下标）。
func Rank(c *Catalog, candidates []int, s Signals, seed int64, now time.Time) []int {
	sc := newScorer(c, s, now)
	// 按分组大小开方归一：作品多的分组不会只因数量多就霸占推荐，小分组也有露面机会。
	sizes := make([]int32, len(c.Names))
	for _, i := range candidates {
		if g := c.Items[i].Group; g >= 0 {
			sizes[g]++
		}
	}
	type keyed struct {
		idx int32
		key float64
	}
	list := make([]keyed, len(candidates))
	for k, i := range candidates {
		it := &c.Items[i]
		w := sc.weight(it)
		if it.Group >= 0 && sizes[it.Group] > 1 {
			w /= math.Sqrt(float64(sizes[it.Group]))
		}
		// log(u)/w 与 u^(1/w) 同序，数值更稳定。
		list[k] = keyed{int32(i), math.Log(unit(seed, it.ID)) / w}
	}
	sort.Slice(list, func(a, b int) bool { return list[a].key > list[b].key })
	order := make([]int, len(list))
	for k, e := range list {
		order[k] = int(e.idx)
	}
	return Spread(c, order, 2, 24)
}

// Spread 打散：尽量让同一分组不出现在最近 gap 个位置里，最多往后找 look 个。顺序其余部分不变。
// 只在窗口内挪动，耗时 O(n·look)。
func Spread(c *Catalog, order []int, gap, look int) []int {
	for i := range order {
		for j := i; j < len(order) && j < i+look; j++ {
			if !recent(c, order[:i], c.Items[order[j]].Group, gap) {
				if j > i {
					picked := order[j]
					copy(order[i+1:j+1], order[i:j])
					order[i] = picked
				}
				break
			}
		}
	}
	return order
}

func recent(c *Catalog, out []int, group int32, gap int) bool {
	if group < 0 {
		return false
	}
	for k := len(out) - 1; k >= 0 && k >= len(out)-gap; k-- {
		if c.Items[out[k]].Group == group {
			return true
		}
	}
	return false
}
