package btmanga

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ReleasePage separates a recognizable work from publishers' torrent titles.
// Warnings retain source failures even when another source returned results.
type ReleasePage struct {
	Subject  Subject   `json:"subject"`
	Items    []Release `json:"items"`
	Keywords []string  `json:"keywords"`
	Warnings []string  `json:"warnings"`
	Page     int       `json:"page"`
	HasMore  bool      `json:"has_more"`
}

// SearchReleases uses catalog names to query DMHY, Nyaa, and APIBay's comic
// category. Labels retain the publishers' claims and partial source failures.
func SearchReleases(ctx context.Context, subject Subject, category string, page int, proxy string) (ReleasePage, error) {
	out := ReleasePage{Subject: subject, Items: []Release{}, Keywords: subject.searchTerms(), Warnings: []string{}, Page: page}
	if page < 1 || page > 100 || len(out.Keywords) == 0 {
		return out, errors.New("漫画名称或页码无效")
	}
	client, close, err := remoteClient(proxy)
	if err != nil {
		return out, err
	}
	defer close()
	terms := []string{}
	size := 0
	for _, term := range out.Keywords {
		if size+len(term)+3 > 300 {
			break
		}
		terms = append(terms, `"`+term+`"`)
		size += len(term) + 3
	}
	nyaaCategory := category
	if category == "zh" {
		nyaaCategory = "3_2"
	}
	type result struct {
		items []Release
		err   error
		nyaa  bool
	}
	ctx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	results := make(chan result, 3)
	go func() {
		items, err := searchNyaa(ctx, client, strings.Join(terms, "|"), nyaaCategory, page)
		results <- result{items: items, err: err, nyaa: true}
	}()
	count := 1
	if page == 1 && (category == "3_0" || category == "zh") {
		count += 2
		go func() {
			items, err := searchDMHY(ctx, client, editionSuffix.ReplaceAllString(subject.Title, ""))
			results <- result{items: items, err: err}
		}()
		go func() {
			items, err := searchAPIBay(ctx, client, subject, category)
			results <- result{items: items, err: err}
		}()
	}
	succeeded := 0
	seen := map[string]bool{}
	for range count {
		r := <-results
		if r.err != nil {
			out.Warnings = append(out.Warnings, r.err.Error())
			continue
		}
		succeeded++
		if r.nyaa {
			out.HasMore = len(r.items) >= 75
		}
		for _, item := range r.items {
			if r.nyaa && (!matchesSelectedTitle(item.Title, out.Keywords) || nonComicFormat.MatchString(item.Title)) {
				continue
			}
			if seen[item.Torrent] || (category == "zh" && !chineseRelease(item.Language)) {
				continue
			}
			seen[item.Torrent] = true
			out.Items = append(out.Items, item)
		}
	}
	if succeeded == 0 {
		return out, errors.New(strings.Join(out.Warnings, "；"))
	}
	sort.SliceStable(out.Items, func(i, j int) bool {
		a, b := out.Items[i], out.Items[j]
		priority := func(r Release) int {
			if subject.Source == "Open Library" && r.Source == "APIBay" && !chineseRelease(r.Language) {
				return 1
			}
			return releasePriority(r)
		}
		if priority(a) != priority(b) {
			return priority(a) < priority(b)
		}
		return a.Seeders > b.Seeders
	})
	return out, nil
}

// Quoted upstream search still matches possessives and plurals: "The Outcast"
// must not identify "The Outcasts" or "The Outcast's Restaurant" as this work.
func matchesSelectedTitle(title string, names []string) bool {
	title = strings.ToLower(title)
	for _, name := range names {
		name = strings.ToLower(name)
		if name == "" {
			continue
		}
		start, _ := utf8.DecodeRuneInString(name)
		end, _ := utf8.DecodeLastRuneInString(name)
		for offset := 0; offset < len(title); {
			index := strings.Index(title[offset:], name)
			if index < 0 {
				break
			}
			index += offset
			after := index + len(name)
			beforeOK, afterOK := true, true
			if index > 0 && unicode.Is(unicode.Latin, start) {
				previous, _ := utf8.DecodeLastRuneInString(title[:index])
				beforeOK = !unicode.IsLetter(previous) && !unicode.IsDigit(previous)
			}
			if after < len(title) && unicode.Is(unicode.Latin, end) {
				next, _ := utf8.DecodeRuneInString(title[after:])
				afterOK = !unicode.IsLetter(next) && !unicode.IsDigit(next) && next != '\'' && next != '’'
			}
			if beforeOK && afterOK {
				return true
			}
			offset = after
		}
	}
	return false
}

func releasePriority(r Release) int {
	if chineseRelease(r.Language) {
		return 0
	}
	// 中文社区未标注语言的发布仍展示为“语言未标明”，不冒充已确认的译本。
	if r.Source == "动漫花园" {
		return 1
	}
	if strings.HasPrefix(r.Language, "英文") {
		return 2
	}
	return 3
}

func searchDMHY(ctx context.Context, client *http.Client, term string) ([]Release, error) {
	address := "https://share.dmhy.org/topics/rss/rss.xml?" + url.Values{"keyword": {term}, "sort_id": {"3"}}.Encode()
	data, err := remoteRead(ctx, client, address, nil, 2<<20, "动漫花园")
	if err != nil {
		return nil, err
	}
	var rss struct {
		XMLName xml.Name `xml:"rss"`
		Channel struct {
			Items []struct {
				Title     string `xml:"title"`
				Link      string `xml:"link"`
				Category  string `xml:"category"`
				Date      string `xml:"pubDate"`
				Enclosure struct {
					URL string `xml:"url,attr"`
				} `xml:"enclosure"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err = xml.Unmarshal(data, &rss); err != nil {
		return nil, errors.New("动漫花园没有返回有效资源列表")
	}
	out := []Release{}
	for _, item := range rss.Channel.Items {
		u, e := url.Parse(item.Enclosure.URL)
		p, pe := url.Parse(item.Link)
		if e != nil || u.Scheme != "magnet" || len(item.Enclosure.URL) > 8192 || !infoHash.MatchString(u.Query().Get("xt")) || pe != nil || p.Host != "share.dmhy.org" || (p.Scheme != "https" && p.Scheme != "http") || !strings.HasPrefix(p.Path, "/topics/view/") || len(item.Title) > 2000 || adultRelease.MatchString(item.Title) {
			continue
		}
		p.Scheme = "https"
		out = append(out, Release{Title: item.Title, Torrent: item.Enclosure.URL, Page: p.String(), Category: item.Category, Date: item.Date, Seeders: -1, Source: "动漫花园", Language: releaseLanguage(item.Title, item.Category)})
		if len(out) == 100 {
			break
		}
	}
	return out, nil
}

var infoHash = regexp.MustCompile(`(?i)^urn:btih:([a-f0-9]{40}|[a-z2-7]{32})$`)
var adultRelease = regexp.MustCompile(`(?i)\b(hentai|nsfw|r-?18|erotica|erotic|pornographic|pornography)\b|18禁|成人漫画|成人漫畫|无修正|無修正|全步兵|去衣|果体|裸体`)
var chineseMarker = regexp.MustCompile(`(?i)中文|汉化|漢化|简体|繁体|簡體|繁體|简中|繁中|簡中|chinese|\b(chs|cht|big5|gb2312)\b`)
var englishMarker = regexp.MustCompile(`(?i)\b(english|eng)\b`)
var nonComicFormat = regexp.MustCompile(`(?i)\[(?:[^\[\]]*\s)?(?:web|light)\s+novels?\]|\((?:web|light)\s+novels?\)|\.(epub|mobi|azw3|mp3|mp4)\b`)

func releaseLanguage(title, category string) string {
	if category == "港台原版" {
		return "繁体中文（分类标注）"
	}
	if chineseMarker.MatchString(title) {
		return "中文（标题标注）"
	}
	if strings.HasSuffix(category, " - English-translated") {
		return "英文译本"
	}
	if englishMarker.MatchString(title) {
		return "英文（标题标注）"
	}
	if category == "日文原版" {
		return "日文原版"
	}
	if strings.HasSuffix(category, " - Raw") {
		return "原文"
	}
	return "语言未标明"
}

func chineseRelease(language string) bool {
	return strings.Contains(language, "中文")
}
