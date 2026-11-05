package btmanga

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	_ "golang.org/x/image/webp"
)

// Subject identifies a comic in a supported catalog. Key is unique across
// sources; ID retains the legacy Bangumi number. The cover identifies the work,
// not a particular torrent or a verified downloadable edition.
type Subject struct {
	ID       int      `json:"id"`
	Key      string   `json:"key"`
	Source   string   `json:"source"`
	PageURL  string   `json:"page_url"`
	Origin   string   `json:"origin"`
	Title    string   `json:"title"`
	Original string   `json:"original"`
	Summary  string   `json:"summary"`
	Cover    string   `json:"cover"`
	Author   string   `json:"author"`
	Volumes  string   `json:"volumes"`
	Tags     []string `json:"tags"`
	Names    []string `json:"names"`
}

// CatalogPage contains a bounded page of comic metadata in the source's language.
type CatalogPage struct {
	Items    []Subject `json:"items"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	Size     int       `json:"size"`
	HasMore  bool      `json:"has_more"`
	Warnings []string  `json:"warnings"`
}

type catalogSubject struct {
	ID       int      `json:"id"`
	Type     int      `json:"type"`
	Name     string   `json:"name"`
	NameCN   string   `json:"name_cn"`
	Summary  string   `json:"summary"`
	NSFW     bool     `json:"nsfw"`
	Platform string   `json:"platform"`
	Tags     []string `json:"meta_tags"`
	Images   struct {
		Common string `json:"common"`
	} `json:"images"`
	Infobox []struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	} `json:"infobox"`
}

func searchBangumi(ctx context.Context, query string, page, size int, proxy string) (CatalogPage, error) {
	out := CatalogPage{Items: []Subject{}, Page: page, Size: size, Warnings: []string{}}
	query = strings.TrimSpace(query)
	if len(query) > 300 || page < 1 || page > 125 {
		return out, errors.New("漫画名称或页码无效")
	}
	client, close, err := remoteClient(proxy)
	if err != nil {
		return out, err
	}
	defer close()
	order := "match"
	if query == "" {
		order = "heat"
	}
	filter := map[string]any{"type": []int{1}, "nsfw": false}
	// 老条目可能没有新式公共标签（例如《灌篮高手》原版），关键词
	// 搜索不能靠 meta_tags 排除它们；返回后仍按平台与 NSFW 标记过滤。
	if query == "" {
		filter["meta_tags"] = []string{"漫画"}
	}
	body, err := json.Marshal(map[string]any{"keyword": query, "sort": order, "filter": filter})
	if err != nil {
		return out, err
	}
	address := fmt.Sprintf("https://api.bgm.tv/v0/search/subjects?limit=%d&offset=%d", out.Size, (page-1)*out.Size)
	data, err := remoteRead(ctx, client, address, body, 2<<20, "Bangumi 漫画目录")
	if err != nil {
		return out, err
	}
	var raw struct {
		Total int              `json:"total"`
		Data  []catalogSubject `json:"data"`
	}
	if err = json.Unmarshal(data, &raw); err != nil {
		return out, errors.New("漫画目录没有返回有效列表")
	}
	out.Total = min(raw.Total, 1000)
	out.HasMore = page*size < out.Total
	for _, item := range raw.Data {
		if s, ok := item.subject(); ok {
			out.Items = append(out.Items, s)
		}
	}
	sort.SliceStable(out.Items, func(i, j int) bool {
		return strings.EqualFold(out.Items[i].Title, query) && !strings.EqualFold(out.Items[j].Title, query)
	})
	return out, nil
}

// GetSubject resolves a selected catalog identity again before looking for
// releases, retaining the same non-NSFW manga boundary as the search page.
func GetSubject(ctx context.Context, id int, proxy string) (Subject, error) {
	if id < 1 || id > 100000000 {
		return Subject{}, errors.New("漫画编号无效")
	}
	client, close, err := remoteClient(proxy)
	if err != nil {
		return Subject{}, err
	}
	defer close()
	data, err := remoteRead(ctx, client, "https://api.bgm.tv/v0/subjects/"+strconv.Itoa(id), nil, 1<<20, "Bangumi 漫画详情")
	if err != nil {
		return Subject{}, err
	}
	var raw catalogSubject
	if err = json.Unmarshal(data, &raw); err != nil {
		return Subject{}, errors.New("漫画详情无效")
	}
	s, ok := raw.subject()
	if !ok || s.ID != id {
		return Subject{}, errors.New("这个条目不是普通漫画")
	}
	return s, nil
}

func (r catalogSubject) subject() (Subject, bool) {
	isManga := r.Platform == "漫画"
	for _, tag := range r.Tags {
		isManga = isManga || tag == "漫画"
	}
	if r.ID < 1 || r.Type != 1 || r.NSFW || !isManga || strings.TrimSpace(r.Name) == "" {
		return Subject{}, false
	}
	s := Subject{ID: r.ID, Key: "bangumi:" + strconv.Itoa(r.ID), Source: "Bangumi", PageURL: "https://bgm.tv/subject/" + strconv.Itoa(r.ID), Title: strings.TrimSpace(r.NameCN), Original: r.Name, Summary: r.Summary, Tags: append([]string{}, r.Tags...), Names: []string{r.Name}}
	for _, tag := range r.Tags {
		if tag == "中国" || tag == "日本" || tag == "美国" || tag == "英国" || tag == "法国" || tag == "韩国" {
			s.Origin = tag
			break
		}
	}
	if s.Title == "" {
		s.Title = r.Name
	}
	s.Names = append(s.Names, s.Title)
	if ValidCoverURL(r.Images.Common) {
		s.Cover = r.Images.Common
	}
	for _, entry := range r.Infobox {
		values := infoValues(entry.Value)
		switch entry.Key {
		case "作者", "著者":
			s.Author = strings.Join(values, "、")
		case "册数":
			s.Volumes = strings.Join(values, "、")
		case "别名":
			s.Names = append(s.Names, values...)
		}
	}
	return s, true
}

func infoValues(raw json.RawMessage) []string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []string{text}
	}
	var values []struct {
		Value string `json:"v"`
	}
	if json.Unmarshal(raw, &values) != nil {
		return nil
	}
	out := []string{}
	for _, v := range values {
		if v.Value != "" && len(out) < 12 {
			out = append(out, v.Value)
		}
	}
	return out
}

var editionSuffix = regexp.MustCompile(`(?i)\s+(完全版|文庫版|爱藏版|愛蔵版|新装版|新裝版)$`)

func (s Subject) searchTerms() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, name := range s.Names {
		name = editionSuffix.ReplaceAllString(strings.TrimSpace(name), "")
		name = strings.Join(strings.Fields(strings.NewReplacer(`"`, " ", "|", " ", "(", " ", ")", " ").Replace(name)), " ")
		if len(name) < 2 || len(name) > 100 || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		out = append(out, name)
		if len(out) == 6 {
			break
		}
	}
	return out
}

// ValidCoverURL limits the image proxy to supported catalogs' public cover paths.
func ValidCoverURL(address string) bool {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || strings.Contains(u.Path, "..") {
		return false
	}
	switch u.Host {
	case "lain.bgm.tv":
		return u.RawQuery == "" && coverPath.MatchString(u.Path)
	case "uploads.mangadex.org":
		return u.RawQuery == "" && mangaDexCoverPath.MatchString(u.Path)
	case "covers.openlibrary.org":
		return (u.RawQuery == "" || u.RawQuery == "default=false") && openLibraryCoverPath.MatchString(u.Path)
	}
	return false
}

var coverPath = regexp.MustCompile(`^/(r/[0-9]+/)?pic/cover/[a-z]/[a-f0-9]{2}/[a-f0-9]{2}/[A-Za-z0-9_]+\.(jpg|jpeg|png|webp)$`)
var archiveImageHost = regexp.MustCompile(`^ia[0-9]{6}\.(us|eu)\.archive\.org$`)

// Some Open Library covers live in Archive.org's cover ZIPs. Follow only the
// exact original cover ID, never an arbitrary ZIP, member, or redirect target.
func validCoverRedirect(address, original string) bool {
	u, err := url.Parse(address)
	origin, originalErr := url.Parse(original)
	if err != nil || originalErr != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" {
		return false
	}
	if u.Host == origin.Host && ValidCoverURL(address) {
		return true
	}
	if origin.Host != "covers.openlibrary.org" || !ValidCoverURL(original) {
		return false
	}
	id, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(origin.Path, "/b/id/"), "-M.jpg"), 10, 64)
	if err != nil || id <= 0 {
		return false
	}
	group := fmt.Sprintf("m_covers_%04d", id/1000000)
	archive := fmt.Sprintf("%s_%02d.zip", group, id/10000%100)
	file := fmt.Sprintf("%010d-M.jpg", id)
	if u.Host == "archive.org" {
		return u.RawQuery == "" && u.Path == "/download/"+group+"/"+archive+"/"+file
	}
	if !archiveImageHost.MatchString(u.Host) || u.Path != "/view_archive.php" {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q) != 2 || len(q["archive"]) != 1 || len(q["file"]) != 1 || q.Get("file") != file {
		return false
	}
	parts := strings.Split(q.Get("archive"), "/")
	if len(parts) != 5 || parts[0] != "" || parts[2] != "items" || parts[3] != group || parts[4] != archive {
		return false
	}
	shard, err := strconv.Atoi(parts[1])
	return err == nil && shard >= 0 && shard <= 999
}

// FetchCover reads a bounded raster image. Only validated CDN redirects and
// the same Open Library cover's Archive.org image member are followed.
func FetchCover(ctx context.Context, address, proxy string) ([]byte, string, error) {
	if !ValidCoverURL(address) {
		return nil, "", errors.New("漫画封面地址无效")
	}
	client, close, err := remoteClient(proxy)
	if err != nil {
		return nil, "", err
	}
	defer close()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 4 || !validCoverRedirect(req.URL.String(), address) {
			return http.ErrUseLastResponse
		}
		return nil
	}
	data, err := remoteRead(ctx, client, address, nil, 4<<20, "漫画封面")
	if err != nil {
		return nil, "", err
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 16000000 || (format != "jpeg" && format != "png" && format != "webp") {
		return nil, "", errors.New("漫画封面不是有效图片")
	}
	return data, http.DetectContentType(data), nil
}
