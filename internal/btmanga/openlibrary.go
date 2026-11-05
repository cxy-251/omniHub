package btmanga

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var openLibraryID = regexp.MustCompile(`^OL[0-9]{1,12}W$`)
var openLibraryCoverPath = regexp.MustCompile(`^/b/id/[0-9]{1,12}-M\.jpg$`)

type openLibraryWork struct {
	Key           string          `json:"key"`
	Title         string          `json:"title"`
	AuthorNames   []string        `json:"author_name"`
	CoverID       int64           `json:"cover_i"`
	Covers        []int64         `json:"covers"`
	Subjects      []string        `json:"subjects"`
	SearchSubject []string        `json:"subject"`
	FirstSentence json.RawMessage `json:"first_sentence"`
	Description   json.RawMessage `json:"description"`
}

func searchOpenLibrary(ctx context.Context, q catalogQuery, page, size int, proxy string) (CatalogPage, error) {
	out := CatalogPage{Items: []Subject{}, Page: page, Size: size, Warnings: []string{}}
	client, close, err := remoteClient(proxy)
	if err != nil {
		return out, err
	}
	defer close()
	// Search titles, not arbitrary author text, and require comic subjects. This
	// excludes novelizations, reference books and unrelated vandalized records.
	titles := []string{}
	for _, term := range append([]string{q.text}, q.aliases...) {
		if term != "" {
			titles = append(titles, "title:"+solrQuote(term))
		}
	}
	query := `(subject:"comics" OR subject:"graphic novels" OR subject:"comic books") AND NOT (subject:erotica OR subject:pornography OR subject:hentai)`
	if len(titles) > 0 {
		query = "(" + strings.Join(titles, " OR ") + ") AND " + query
	}
	values := url.Values{"q": {query}, "limit": {strconv.Itoa(size)}, "page": {strconv.Itoa(page)}, "lang": {"zh"}, "fields": {"key,title,author_name,cover_i,subject,first_sentence"}}
	data, err := remoteRead(ctx, client, "https://openlibrary.org/search.json?"+values.Encode(), nil, 2<<20, "Open Library")
	if err != nil {
		return out, err
	}
	var raw struct {
		Total int               `json:"numFound"`
		Docs  []openLibraryWork `json:"docs"`
	}
	if err = json.Unmarshal(data, &raw); err != nil {
		return out, errors.New("Open Library 没有返回有效漫画列表")
	}
	out.Total = min(raw.Total, 1000)
	out.HasMore = page*size < out.Total
	for _, record := range raw.Docs {
		if s, ok := record.subject(); ok {
			out.Items = append(out.Items, s)
		}
	}
	return out, nil
}

func getOpenLibrary(ctx context.Context, id, proxy string) (Subject, error) {
	if !openLibraryID.MatchString(id) {
		return Subject{}, errors.New("Open Library 漫画编号无效")
	}
	client, close, err := remoteClient(proxy)
	if err != nil {
		return Subject{}, err
	}
	defer close()
	data, err := remoteRead(ctx, client, "https://openlibrary.org/works/"+id+".json", nil, 1<<20, "Open Library")
	if err != nil {
		return Subject{}, err
	}
	var raw openLibraryWork
	if err = json.Unmarshal(data, &raw); err != nil {
		return Subject{}, errors.New("Open Library 漫画详情无效")
	}
	s, ok := raw.subject()
	if !ok || raw.Key != "/works/"+id {
		return Subject{}, errors.New("这个条目不是普通漫画")
	}
	return s, nil
}

func (r openLibraryWork) subject() (Subject, bool) {
	id := strings.TrimPrefix(r.Key, "/works/")
	if !openLibraryID.MatchString(id) || r.Title == "" || adultRelease.MatchString(r.Title) {
		return Subject{}, false
	}
	subjects := append(append([]string{}, r.Subjects...), r.SearchSubject...)
	comic := false
	for _, subject := range subjects {
		lower := strings.ToLower(subject)
		if strings.Contains(lower, "erotic") || strings.Contains(lower, "erotica") || strings.Contains(lower, "pornograph") || strings.Contains(lower, "hentai") {
			return Subject{}, false
		}
		comic = comic || strings.Contains(lower, "comic") || strings.Contains(lower, "graphic novel")
	}
	if !comic {
		return Subject{}, false
	}
	s := Subject{Key: "openlibrary:" + id, Source: "Open Library", PageURL: "https://openlibrary.org/works/" + id, Title: r.Title, Original: r.Title, Author: strings.Join(r.AuthorNames, "、"), Tags: []string{}, Names: []string{r.Title}}
	s.Summary = libraryText(r.Description)
	if s.Summary == "" {
		s.Summary = libraryText(r.FirstSentence)
	}
	if r.CoverID <= 0 && len(r.Covers) > 0 {
		r.CoverID = r.Covers[0]
	}
	if r.CoverID > 0 {
		s.Cover = fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-M.jpg?default=false", r.CoverID)
	}
	for _, tag := range subjects {
		if len(s.Tags) < 5 {
			s.Tags = append(s.Tags, tag)
		}
	}
	return s, true
}

func libraryText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var entry struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &entry) == nil {
		return entry.Value
	}
	var texts []string
	if json.Unmarshal(raw, &texts) == nil {
		return strings.Join(texts, "\n")
	}
	return ""
}

func solrQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\r", " ", "\n", " ").Replace(s) + `"`
}
