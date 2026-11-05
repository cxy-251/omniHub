package btmanga

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const uuidPattern = `[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`

var mangaDexID = regexp.MustCompile(`^` + uuidPattern + `$`)
var mangaDexCoverPath = regexp.MustCompile(`^/covers/` + uuidPattern + `/` + uuidPattern + `\.(jpg|png)(\.256\.jpg)?$`)

type mangaDexRecord struct {
	ID         string `json:"id"`
	Attributes struct {
		Title            map[string]string   `json:"title"`
		AltTitles        []map[string]string `json:"altTitles"`
		Description      map[string]string   `json:"description"`
		ContentRating    string              `json:"contentRating"`
		OriginalLanguage string              `json:"originalLanguage"`
		LastVolume       string              `json:"lastVolume"`
		Tags             []struct {
			Attributes struct {
				Name map[string]string `json:"name"`
			} `json:"attributes"`
		} `json:"tags"`
	} `json:"attributes"`
	Relationships []struct {
		Type       string `json:"type"`
		Attributes struct {
			Name     string `json:"name"`
			FileName string `json:"fileName"`
		} `json:"attributes"`
	} `json:"relationships"`
}

func searchMangaDex(ctx context.Context, q catalogQuery, page, size int, proxy string) (CatalogPage, error) {
	out := CatalogPage{Items: []Subject{}, Page: page, Size: size, Warnings: []string{}}
	client, close, err := remoteClient(proxy)
	if err != nil {
		return out, err
	}
	defer close()
	values := url.Values{"limit": {strconv.Itoa(size)}, "offset": {strconv.Itoa((page - 1) * size)}, "includes[]": {"cover_art", "author"}, "contentRating[]": {"safe", "suggestive"}}
	if q.text != "" {
		values.Set("title", q.text)
		values.Set("order[relevance]", "desc")
	} else {
		values.Set("order[followedCount]", "desc")
	}
	data, err := remoteRead(ctx, client, "https://api.mangadex.org/manga?"+values.Encode(), nil, 2<<20, "MangaDex")
	if err != nil {
		return out, err
	}
	var raw struct {
		Result string           `json:"result"`
		Total  int              `json:"total"`
		Data   []mangaDexRecord `json:"data"`
	}
	if err = json.Unmarshal(data, &raw); err != nil || raw.Result != "ok" {
		return out, errors.New("MangaDex 没有返回有效漫画列表")
	}
	out.Total = min(raw.Total, 1000)
	out.HasMore = page*size < out.Total
	for _, record := range raw.Data {
		if s, ok := record.subject(); ok {
			out.Items = append(out.Items, s)
		}
	}
	return out, nil
}

func getMangaDex(ctx context.Context, id, proxy string) (Subject, error) {
	if !mangaDexID.MatchString(id) {
		return Subject{}, errors.New("MangaDex 漫画编号无效")
	}
	client, close, err := remoteClient(proxy)
	if err != nil {
		return Subject{}, err
	}
	defer close()
	data, err := remoteRead(ctx, client, "https://api.mangadex.org/manga/"+id+"?includes[]=cover_art&includes[]=author", nil, 1<<20, "MangaDex")
	if err != nil {
		return Subject{}, err
	}
	var raw struct {
		Result string         `json:"result"`
		Data   mangaDexRecord `json:"data"`
	}
	if err = json.Unmarshal(data, &raw); err != nil || raw.Result != "ok" {
		return Subject{}, errors.New("MangaDex 漫画详情无效")
	}
	s, ok := raw.Data.subject()
	if !ok || raw.Data.ID != id {
		return Subject{}, errors.New("这个条目不是普通漫画")
	}
	return s, nil
}

func (r mangaDexRecord) subject() (Subject, bool) {
	a := r.Attributes
	if !mangaDexID.MatchString(r.ID) || (a.ContentRating != "safe" && a.ContentRating != "suggestive") {
		return Subject{}, false
	}
	names := []map[string]string{a.Title}
	names = append(names, a.AltTitles...)
	lookup := func(languages ...string) string {
		for _, lang := range languages {
			for _, name := range names {
				if s := strings.TrimSpace(name[lang]); s != "" {
					return s
				}
			}
		}
		return ""
	}
	original := lookup(a.OriginalLanguage, a.OriginalLanguage+"-ro", "en", "ja", "ja-ro", "zh", "ko", "ko-ro")
	title := lookup("zh", "zh-hk", "zh-tw", "en")
	if title == "" {
		title = original
	}
	if title == "" {
		return Subject{}, false
	}
	s := Subject{Key: "mangadex:" + r.ID, Source: "MangaDex", PageURL: "https://mangadex.org/title/" + r.ID, Title: title, Original: original, Volumes: a.LastVolume, Names: []string{}, Tags: []string{}}
	for _, language := range []string{"zh", "zh-hk", "zh-tw", "en"} {
		if summary := a.Description[language]; summary != "" {
			s.Summary = summary
			break
		}
	}
	for _, name := range []string{lookup("en"), original, title} {
		if name != "" {
			s.Names = append(s.Names, name)
		}
	}
	for _, name := range a.AltTitles {
		for _, lang := range []string{"en", "zh", "zh-hk", "ja", "ja-ro", "ko", "ko-ro"} {
			if v := name[lang]; v != "" && len(s.Names) < 16 {
				s.Names = append(s.Names, v)
			}
		}
	}
	for _, relation := range r.Relationships {
		if relation.Type == "author" && s.Author == "" {
			s.Author = relation.Attributes.Name
		}
		if relation.Type == "cover_art" {
			cover := "https://uploads.mangadex.org/covers/" + r.ID + "/" + relation.Attributes.FileName + ".256.jpg"
			if ValidCoverURL(cover) {
				s.Cover = cover
			}
		}
	}
	for _, tag := range a.Tags {
		if name := tag.Attributes.Name["en"]; name != "" && len(s.Tags) < 8 {
			s.Tags = append(s.Tags, name)
		}
	}
	switch a.OriginalLanguage {
	case "zh", "zh-hk", "zh-tw":
		s.Origin = "中文原创"
	case "ja":
		s.Origin = "日语原创"
	case "en":
		s.Origin = "英语原创"
	case "ko":
		s.Origin = "韩语原创"
	}
	return s, true
}
