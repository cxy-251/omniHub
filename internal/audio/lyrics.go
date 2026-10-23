package audio

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// LyricLine 是一行歌词；Time 是开始的秒数，纯文本歌词为 -1。
type LyricLine struct {
	Time float64 `json:"time"`
	Text string  `json:"text"`
}

// Lyrics 是解析后的歌词：Synced 表示带时间轴（LRC）。
type Lyrics struct {
	Synced bool        `json:"synced"`
	Lines  []LyricLine `json:"lines"`
}

var lrcStamp = regexp.MustCompile(`^\[(\d{1,3}):(\d{1,2}(?:[.:]\d{1,3})?)\]`)

// ParseLyrics 认 LRC：一行可有多个时间戳（[00:12.30][01:05.00]歌词），[ar:…] 这类信息行忽略；
// 有时间戳的行不到一半就当纯文本，按原样分行。
func ParseLyrics(text string) Lyrics {
	text = strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n")
	if text == "" {
		return Lyrics{Lines: []LyricLine{}}
	}
	raw := strings.Split(text, "\n")
	var timed []LyricLine
	stamped := 0
	for _, line := range raw {
		var times []float64
		for {
			m := lrcStamp.FindStringSubmatch(line)
			if m == nil {
				break
			}
			minutes, _ := strconv.Atoi(m[1])
			seconds, _ := strconv.ParseFloat(strings.Replace(m[2], ":", ".", 1), 64)
			times = append(times, float64(minutes)*60+seconds)
			line = line[len(m[0]):]
		}
		if len(times) > 0 {
			stamped++
		}
		for _, t := range times {
			timed = append(timed, LyricLine{Time: t, Text: strings.TrimSpace(line)})
		}
	}
	if stamped*2 >= len(raw) && len(timed) > 0 {
		sort.SliceStable(timed, func(i, j int) bool { return timed[i].Time < timed[j].Time })
		return Lyrics{Synced: true, Lines: timed}
	}
	lines := make([]LyricLine, 0, len(raw))
	for _, line := range raw {
		lines = append(lines, LyricLine{Time: -1, Text: strings.TrimSpace(line)})
	}
	return Lyrics{Lines: lines}
}
