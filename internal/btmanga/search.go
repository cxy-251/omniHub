// Package btmanga searches public comic releases and downloads selected releases
// with an optional system aria2. Search never starts downloads.
package btmanga

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Release describes the source's claims, not a verified language or completeness.
type Release struct {
	Title    string `json:"title" xml:"title"`
	Torrent  string `json:"torrent" xml:"link"`
	Page     string `json:"page" xml:"guid"`
	Size     string `json:"size" xml:"size"`
	Seeders  int    `json:"seeders" xml:"seeders"`
	Category string `json:"category" xml:"category"`
	Date     string `json:"date" xml:"pubDate"`
	Source   string `json:"source"`
	Language string `json:"language"`
}

// Search queries Nyaa's non-Sukebei literature feed, bounded to one result page.
func Search(ctx context.Context, query, category string, page int, proxy string) ([]Release, error) {
	client, close, err := remoteClient(proxy)
	if err != nil {
		return nil, err
	}
	defer close()
	return searchNyaa(ctx, client, query, category, page)
}

func searchNyaa(ctx context.Context, client *http.Client, query, category string, page int) ([]Release, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 300 || page < 1 || page > 100 {
		return nil, errors.New("请输入漫画名称（也可用英文名）")
	}
	if category != "3_1" && category != "3_2" && category != "3_3" {
		category = "3_0"
	}
	u := "https://nyaa.si/?" + url.Values{"page": {"rss"}, "q": {query}, "c": {category}, "p": {strconv.Itoa(page)}, "s": {"seeders"}, "o": {"desc"}}.Encode()
	data, err := remoteRead(ctx, client, u, nil, 2<<20, "Nyaa")
	if err != nil {
		return nil, err
	}
	var feed struct {
		XMLName xml.Name `xml:"rss"`
		Channel struct {
			Items []Release `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, errors.New("资源站没有返回有效的搜索列表")
	}
	out := make([]Release, 0, min(len(feed.Channel.Items), 100))
	for _, r := range feed.Channel.Items {
		u, err := url.Parse(r.Torrent)
		if err != nil || u.Scheme != "https" || u.Host != "nyaa.si" || !strings.HasPrefix(u.Path, "/download/") || len(r.Title) > 2000 || adultRelease.MatchString(r.Title) {
			continue
		}
		r.Source = "Nyaa"
		r.Language = releaseLanguage(r.Title, r.Category)
		out = append(out, r)
		if len(out) == 100 {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seeders > out[j].Seeders })
	return out, nil
}
