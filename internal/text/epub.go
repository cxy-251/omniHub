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
	Items  []struct {
		ID   string `xml:"id,attr"`
		Href string `xml:"href,attr"`
		Type string `xml:"media-type,attr"`
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
	return book, nil
}
