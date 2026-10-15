package text

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"omnihub/internal/archiveutil"
)

// EpubChapter identifies an ordered reading document inside the container.
type EpubChapter struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Entry string `json:"entry"`
}

// EpubBook contains bounded metadata and the reading order from the OPF spine.
type EpubBook struct {
	Title    string        `json:"title"`
	Author   string        `json:"author"`
	Cover    string        `json:"cover,omitempty"` // 封面图片在容器内的条目路径
	Chapters []EpubChapter `json:"chapters"`
}

type epubContainer struct {
	Rootfiles []struct {
		Path string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

type epubPackage struct {
	Title  string `xml:"metadata>title"`
	Author string `xml:"metadata>creator"`
	Metas  []struct {
		Name    string `xml:"name,attr"`
		Content string `xml:"content,attr"`
	} `xml:"metadata>meta"`
	Items []struct {
		ID         string `xml:"id,attr"`
		Href       string `xml:"href,attr"`
		Type       string `xml:"media-type,attr"`
		Properties string `xml:"properties,attr"`
	} `xml:"manifest>item"`
	Spine []struct {
		ID     string `xml:"idref,attr"`
		Linear string `xml:"linear,attr"`
	} `xml:"spine>itemref"`
}

func epubEntry(base, reference string) (string, error) {
	u, err := url.Parse(reference)
	if err != nil || u.IsAbs() || u.Host != "" || u.RawQuery != "" || strings.HasPrefix(u.Path, "/") || strings.ContainsAny(u.Path, "\\\x00:") {
		return "", errors.New("invalid EPUB resource reference")
	}
	entry := path.Clean(path.Join(base, u.Path))
	if entry == "." || entry == ".." || strings.HasPrefix(entry, "../") {
		return "", errors.New("EPUB resource escapes container")
	}
	return entry, nil
}

// InspectEpub reads bounded container/OPF metadata in place. It follows the spine,
// not ZIP order, rejects missing/ambiguous chapters and never extracts resources.
func InspectEpub(ctx context.Context, filePath string) (_ *EpubBook, result error) {
	if strings.ToLower(filepath.Ext(filePath)) != ".epub" {
		return nil, errors.New("unsupported EPUB format")
	}
	zr, err := archiveutil.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { result = errors.Join(result, zr.Close()) }()
	entries := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		entries[f.Name] = f
	}
	read := func(name string, limit int64) ([]byte, error) {
		f := entries[name]
		if f == nil {
			return nil, fmt.Errorf("EPUB entry missing: %s", name)
		}
		return archiveutil.ReadEntry(ctx, f, limit)
	}
	containerData, err := read("META-INF/container.xml", 256<<10)
	if err != nil {
		return nil, err
	}
	var container epubContainer
	if err := xml.NewDecoder(bytes.NewReader(containerData)).Decode(&container); err != nil {
		return nil, fmt.Errorf("invalid EPUB container: %w", err)
	}
	if len(container.Rootfiles) == 0 || len(container.Rootfiles) > 16 {
		return nil, errors.New("invalid EPUB rootfiles")
	}
	opf, err := epubEntry("", container.Rootfiles[0].Path)
	if err != nil {
		return nil, err
	}
	packageData, err := read(opf, 2<<20)
	if err != nil {
		return nil, err
	}
	var document epubPackage
	if err := xml.NewDecoder(bytes.NewReader(packageData)).Decode(&document); err != nil {
		return nil, fmt.Errorf("invalid EPUB package: %w", err)
	}
	if len(document.Items) > 20000 || len(document.Spine) > 20000 || len(document.Title) > 4096 || len(document.Author) > 4096 {
		return nil, archiveutil.ErrCapacity
	}
	manifest := make(map[string]int, len(document.Items))
	for i, item := range document.Items {
		if item.ID == "" || len(item.ID) > 512 {
			return nil, errors.New("invalid EPUB manifest ID")
		}
		if _, duplicate := manifest[item.ID]; duplicate {
			return nil, errors.New("duplicate EPUB manifest ID")
		}
		manifest[item.ID] = i
	}
	book := &EpubBook{Title: strings.TrimSpace(document.Title), Author: strings.TrimSpace(document.Author), Chapters: []EpubChapter{}}
	if book.Title == "" {
		book.Title = strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	}
	budget := 0
	for _, reference := range document.Spine {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if reference.Linear == "no" {
			continue
		}
		i, exists := manifest[reference.ID]
		if !exists {
			return nil, errors.New("EPUB spine refers to missing manifest item")
		}
		item := document.Items[i]
		if item.Type != "application/xhtml+xml" && item.Type != "text/html" {
			return nil, errors.New("unsupported EPUB spine content type")
		}
		entry, err := epubEntry(path.Dir(opf), item.Href)
		if err != nil {
			return nil, err
		}
		if entries[entry] == nil {
			return nil, errors.New("EPUB chapter missing from container")
		}
		budget += len(entry) + len(item.ID) + 64
		if budget > 2<<20 {
			return nil, archiveutil.ErrCapacity
		}
		book.Chapters = append(book.Chapters, EpubChapter{ID: item.ID, Title: item.ID, Entry: entry})
	}
	if len(book.Chapters) == 0 {
		return nil, errors.New("EPUB has no readable spine chapters")
	}
	base := path.Dir(opf)
	book.Cover = findCover(&document, base, entries)
	// 章节标题：优先 NCX（EPUB2），其次 nav 文档（EPUB3）；找不到的保留 manifest ID。
	titles := map[string]string{}
	for _, item := range document.Items {
		entry, err := epubEntry(base, item.Href)
		if err != nil || entries[entry] == nil {
			continue
		}
		if item.Type == "application/x-dtbncx+xml" {
			if data, err := read(entry, 2<<20); err == nil {
				ncxTitles(data, path.Dir(entry), titles)
			}
		} else if strings.Contains(item.Properties, "nav") && len(titles) == 0 {
			if data, err := read(entry, 2<<20); err == nil {
				navTitles(data, path.Dir(entry), titles)
			}
		}
	}
	for i := range book.Chapters {
		if t := titles[book.Chapters[i].Entry]; t != "" {
			book.Chapters[i].Title = t
		} else if i == 0 && book.Chapters[i].Entry == book.Cover {
			book.Chapters[i].Title = "封面"
		}
	}
	return book, nil
}

func findCover(doc *epubPackage, base string, entries map[string]*zip.File) string {
	isImage := func(t string) bool { return strings.HasPrefix(t, "image/") }
	resolve := func(href string) string {
		entry, err := epubEntry(base, href)
		if err != nil || entries[entry] == nil {
			return ""
		}
		return entry
	}
	coverID := ""
	for _, m := range doc.Metas {
		if m.Name == "cover" {
			coverID = m.Content
		}
	}
	for _, item := range doc.Items {
		if isImage(item.Type) && (item.ID == coverID && coverID != "" || strings.Contains(item.Properties, "cover-image")) {
			if e := resolve(item.Href); e != "" {
				return e
			}
		}
	}
	for _, item := range doc.Items {
		if isImage(item.Type) && (strings.Contains(strings.ToLower(item.ID), "cover") || strings.Contains(strings.ToLower(item.Href), "cover")) {
			if e := resolve(item.Href); e != "" {
				return e
			}
		}
	}
	return ""
}

// ncxTitles 读取 NCX navPoint 的标签与指向的条目（去掉 #片段），同一条目取第一个标题。
func ncxTitles(data []byte, base string, titles map[string]string) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	var label string
	inText := false
	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "text":
				inText = true
			case "content":
				for _, a := range t.Attr {
					if a.Name.Local == "src" {
						addTitle(titles, base, a.Value, label)
					}
				}
			}
		case xml.EndElement:
			if t.Name.Local == "text" {
				inText = false
			}
		case xml.CharData:
			if inText {
				label = strings.TrimSpace(string(t))
			}
		}
	}
}

// navTitles 读取 EPUB3 nav 文档里的 <a href>标题</a>。
func navTitles(data []byte, base string, titles map[string]string) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	href := ""
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "a" {
				href = ""
				text.Reset()
				for _, a := range t.Attr {
					if a.Name.Local == "href" {
						href = a.Value
					}
				}
			}
		case xml.CharData:
			if href != "" {
				text.Write(t)
			}
		case xml.EndElement:
			if t.Name.Local == "a" && href != "" {
				addTitle(titles, base, href, strings.TrimSpace(text.String()))
				href = ""
			}
		}
	}
}

func addTitle(titles map[string]string, base, href, label string) {
	if label == "" || len(label) > 512 {
		return
	}
	href, _, _ = strings.Cut(href, "#")
	if entry, err := epubEntry(base, href); err == nil && titles[entry] == "" {
		titles[entry] = label
	}
}
