package btmanga

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type catalogQuery struct {
	text    string
	aliases []string
}

type catalogProvider struct {
	id, label string
	search    func(context.Context, catalogQuery, int, int, string) (CatalogPage, error)
	get       func(context.Context, string, string) (Subject, error)
}

var catalogProviders = []catalogProvider{
	{"bangumi", "Bangumi", func(ctx context.Context, q catalogQuery, page, size int, proxy string) (CatalogPage, error) {
		return searchBangumi(ctx, q.text, page, size, proxy)
	}, func(ctx context.Context, id, proxy string) (Subject, error) {
		n, err := strconv.Atoi(id)
		if err != nil {
			return Subject{}, errors.New("漫画编号无效")
		}
		return GetSubject(ctx, n, proxy)
	}},
	{"mangadex", "MangaDex（含国漫）", searchMangaDex, getMangaDex},
	{"openlibrary", "Open Library（含欧美）", searchOpenLibrary, getOpenLibrary},
}

// CatalogSources lists the public catalogs offered by the desktop source picker.
func CatalogSources() []map[string]string {
	out := []map[string]string{{"value": "all", "label": "全部目录"}}
	for _, p := range catalogProviders {
		out = append(out, map[string]string{"value": p.id, "label": p.label})
	}
	return out
}

// SearchCatalog searches all available comic catalogs in a bounded result page.
func SearchCatalog(ctx context.Context, query string, page int, proxy string) (CatalogPage, error) {
	return SearchCatalogSource(ctx, query, page, "all", proxy)
}

// SearchCatalogSource keeps each source's pagination independent. A failed
// source does not discard successful sources; one page contains at most 24 books.
func SearchCatalogSource(ctx context.Context, query string, page int, source, proxy string) (CatalogPage, error) {
	out := CatalogPage{Items: []Subject{}, Page: page, Size: 24, Warnings: []string{}}
	query = strings.TrimSpace(query)
	if len(query) > 300 || strings.ContainsRune(query, '\x00') || page < 1 || page > 125 {
		return out, errors.New("漫画名称或页码无效")
	}
	if source == "" {
		source = "all"
	}
	providers := []catalogProvider{}
	for _, p := range catalogProviders {
		if source == "all" || source == p.id {
			providers = append(providers, p)
		}
	}
	if len(providers) == 0 {
		return out, errors.New("未知漫画目录")
	}
	ctx, cancel := context.WithTimeout(ctx, 22*time.Second)
	defer cancel()
	limit := out.Size / len(providers)
	type result struct {
		page CatalogPage
		err  error
	}
	results := make([]result, len(providers))
	done := make(chan int, len(providers))
	bgmDone := make(chan struct{})
	var bgm CatalogPage
	hasBangumi := source == "all" || source == "bangumi"
	if !hasBangumi {
		close(bgmDone)
	}
	for i, p := range providers {
		go func() {
			q := catalogQuery{text: query}
			if p.id == "openlibrary" && hasHan(query) {
				if hasBangumi && page == 1 {
					select {
					case <-bgmDone:
						q.aliases = latinCatalogNames(query, bgm.Items)
					case <-ctx.Done():
					}
				} else {
					lookup, stop := context.WithTimeout(ctx, 6*time.Second)
					if books, err := searchBangumi(lookup, query, 1, 8, proxy); err == nil {
						q.aliases = latinCatalogNames(query, books.Items)
					}
					stop()
				}
			}
			if (page-1)*limit >= 1000 {
				results[i].page = CatalogPage{Items: []Subject{}}
			} else {
				results[i].page, results[i].err = p.search(ctx, q, page, limit, proxy)
			}
			if p.id == "bangumi" {
				bgm = results[i].page
				close(bgmDone)
			}
			done <- i
		}()
	}
	for range providers {
		<-done
	}
	success := 0
	for i, r := range results {
		if r.err != nil {
			out.Warnings = append(out.Warnings, providers[i].label+"："+r.err.Error())
			continue
		}
		success++
		out.Items = append(out.Items, r.page.Items...)
		out.Total += r.page.Total
		out.HasMore = out.HasMore || r.page.HasMore
		out.Warnings = append(out.Warnings, r.page.Warnings...)
	}
	if success == 0 {
		return out, errors.New(strings.Join(out.Warnings, "；"))
	}
	sort.SliceStable(out.Items, func(i, j int) bool {
		return strings.EqualFold(out.Items[i].Title, query) && !strings.EqualFold(out.Items[j].Title, query)
	})
	return out, nil
}

// GetCatalogSubject resolves a namespaced identity, preventing collisions
// between Bangumi numbers, MangaDex UUIDs, and Open Library work IDs.
func GetCatalogSubject(ctx context.Context, key, proxy string) (Subject, error) {
	if len(key) > 100 {
		return Subject{}, errors.New("漫画编号无效")
	}
	source, id, ok := strings.Cut(key, ":")
	if !ok || id == "" {
		return Subject{}, errors.New("漫画编号无效")
	}
	for _, p := range catalogProviders {
		if p.id == source {
			return p.get(ctx, id, proxy)
		}
	}
	return Subject{}, errors.New("未知漫画目录")
}

func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// Chinese catalog names supply real foreign aliases for the international
// catalog. They are not machine translations or a hard-coded franchise table.
func latinCatalogNames(query string, books []Subject) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, b := range books {
		matches := strings.Contains(strings.ToLower(b.Title), strings.ToLower(query))
		for _, name := range b.Names {
			matches = matches || strings.Contains(strings.ToLower(name), strings.ToLower(query))
		}
		if !matches {
			continue
		}
		for _, name := range b.Names {
			letters, latin := 0, true
			for _, r := range name {
				if unicode.IsLetter(r) {
					letters++
					latin = latin && unicode.Is(unicode.Latin, r)
				}
			}
			if !latin || letters < 3 || len(name) > 100 {
				continue
			}
			// A franchise name before a subtitle also finds other comic editions.
			if prefix, _, ok := strings.Cut(name, ":"); ok && len(prefix) >= 4 {
				name = prefix
			}
			name = strings.TrimSpace(name)
			if !seen[strings.ToLower(name)] {
				seen[strings.ToLower(name)] = true
				out = append(out, name)
			}
			if len(out) == 4 {
				return out
			}
		}
	}
	return out
}
