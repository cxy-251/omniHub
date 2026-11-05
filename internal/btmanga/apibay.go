package btmanga

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var bayID = regexp.MustCompile(`^[1-9][0-9]{0,14}$`)
var bayHash = regexp.MustCompile(`(?i)^[a-f0-9]{40}$`)
var volumeSuffix = regexp.MustCompile(`(?i)\s+(vol(?:ume)?\.?\s*\d.*)$`)

func searchAPIBay(ctx context.Context, client *http.Client, book Subject, category string) ([]Release, error) {
	term := editionSuffix.ReplaceAllString(book.Title, "")
	if category != "zh" {
		for _, name := range book.searchTerms() {
			latin, letters := true, 0
			for _, r := range name {
				if unicode.IsLetter(r) {
					letters++
					latin = latin && unicode.Is(unicode.Latin, r)
				}
			}
			if latin && letters >= 3 {
				term = volumeSuffix.ReplaceAllString(name, "")
				break
			}
		}
	}
	address := "https://apibay.org/q.php?" + url.Values{"q": {term}, "cat": {"602"}}.Encode()
	data, err := remoteRead(ctx, client, address, nil, 2<<20, "APIBay 漫画分类")
	if err != nil {
		return nil, err
	}
	var records []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Hash     string `json:"info_hash"`
		Category string `json:"category"`
		Seeders  string `json:"seeders"`
		Size     string `json:"size"`
		Added    string `json:"added"`
	}
	if err = json.Unmarshal(data, &records); err != nil {
		return nil, errors.New("APIBay 没有返回有效漫画列表")
	}
	out := []Release{}
	for _, r := range records {
		title := html.UnescapeString(r.Name)
		if r.Category != "602" || !bayID.MatchString(r.ID) || !bayHash.MatchString(r.Hash) || strings.Trim(r.Hash, "0") == "" || title == "" || len(title) > 2000 || adultRelease.MatchString(title) {
			continue
		}
		seeders, err := strconv.Atoi(r.Seeders)
		if err != nil || seeders < 0 {
			continue
		}
		magnet := "magnet:?" + url.Values{"xt": {"urn:btih:" + strings.ToUpper(r.Hash)}, "dn": {title}}.Encode()
		size, _ := strconv.ParseInt(r.Size, 10, 64)
		out = append(out, Release{Title: title, Torrent: magnet, Page: "https://thepiratebay.org/description.php?id=" + r.ID, Source: "APIBay", Category: "漫画", Language: releaseLanguage(title, "漫画"), Seeders: seeders, Size: releaseSize(size), Date: r.Added})
		if len(out) == 100 {
			break
		}
	}
	return out, nil
}

func releaseSize(size int64) string {
	if size >= 1<<30 {
		return fmt.Sprintf("%.1f GiB", float64(size)/(1<<30))
	}
	if size > 0 {
		return fmt.Sprintf("%.1f MiB", float64(size)/(1<<20))
	}
	return ""
}
