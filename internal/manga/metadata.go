package manga

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

// ParseMetadata merges bounded JSON and ComicInfo text metadata. Explicit JSON
// fields win, including false/empty values; actual ZIP pages supply chapter counts.
func (r *CBZReader) ParseMetadata() (*PackageMetadata, error) {
	return r.ParseMetadataContext(context.Background())
}

// ParseMetadataContext never resolves external entities, URLs or stylesheets.
// ComicInfo fields follow https://anansi-project.github.io/docs/comicinfo/ .
func (r *CBZReader) ParseMetadataContext(ctx context.Context) (*PackageMetadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var meta PackageMetadata
	var fields map[string]json.RawMessage
	present := map[string]bool{}
	data, jsonErr := r.ReadEntryContext(ctx, "metadata.json")
	if jsonErr != nil && !errors.Is(jsonErr, ErrEntryNotFound) {
		return nil, jsonErr
	}
	if jsonErr == nil {
		if err := json.Unmarshal(data, &fields); err != nil {
			return nil, err
		}
		if fields == nil {
			return nil, errors.New("metadata.json must be an object")
		}
		for key := range fields {
			present[strings.ToLower(key)] = true
		}
		if err := json.Unmarshal(data, &meta); err != nil {
			return nil, err
		}
	}
	data, xmlErr := r.ReadEntryContext(ctx, "ComicInfo.xml")
	if xmlErr != nil && !errors.Is(xmlErr, ErrEntryNotFound) {
		return nil, xmlErr
	}
	if jsonErr != nil && xmlErr != nil {
		return nil, ErrEntryNotFound
	}
	if xmlErr == nil {
		values, err := parseComicInfo(ctx, data)
		if err != nil {
			return nil, err
		}
		if _, set := present["title"]; !set {
			meta.Title = values["Title"]
			if meta.Title == "" {
				meta.Title = values["Series"]
			}
		}
		if _, set := present["author"]; !set {
			meta.Author = values["Writer"]
			if meta.Author == "" {
				meta.Author = values["Penciller"]
			}
		}
		if _, set := present["description"]; !set {
			meta.Description = values["Summary"]
		}
		if _, set := present["tags"]; !set {
			seen := map[string]bool{}
			// Tags is also accepted as an extension used by existing libraries.
			for _, tag := range strings.FieldsFunc(values["Genre"]+","+values["Tags"], func(c rune) bool { return c == ',' || c == ';' }) {
				tag = strings.TrimSpace(tag)
				if tag != "" && !seen[tag] {
					meta.Tags = append(meta.Tags, tag)
					seen[tag] = true
				}
			}
		}
	}
	if _, set := present["local_chapters"]; !set {
		meta.LocalChapters = r.chapterCount()
	}
	if err := validateMetadata(&meta); err != nil {
		return nil, err
	}
	return &meta, ctx.Err()
}

func validateMetadata(meta *PackageMetadata) error {
	if len(meta.ID) > 256 || len(meta.Title) > 4096 || len(meta.Author) > 4096 || len(meta.Description) > 64<<10 || len(meta.Tags) > 512 || meta.LocalChapters < 0 || meta.LocalChapters > 20000 || meta.OnlineChapters < 0 || meta.OnlineChapters > 20000 {
		return errors.New("manga metadata exceeds field capacity")
	}
	for _, tag := range meta.Tags {
		if len(tag) > 256 {
			return errors.New("manga tag exceeds field capacity")
		}
	}
	return nil
}

func (r *CBZReader) chapterCount() int {
	count := 0
	for _, page := range r.pages {
		count = max(count, page.ChapterIndex+1)
	}
	return count
}

func parseComicInfo(ctx context.Context, data []byte) (map[string]string, error) {
	limits := map[string]int{"Title": 4096, "Series": 4096, "Writer": 4096, "Penciller": 4096, "Summary": 64 << 10, "Genre": 128 << 10, "Tags": 128 << 10}
	values := map[string]string{}
	seen := map[string]bool{}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	depth, tokens := 0, 0
	rootSeen, rootClosed := false, false
	field := ""
	var text strings.Builder
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		tokens++
		if tokens > 50000 {
			return nil, errors.New("ComicInfo token capacity exceeded")
		}
		switch value := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				if rootSeen || value.Name.Local != "ComicInfo" {
					return nil, errors.New("invalid ComicInfo root")
				}
				rootSeen = true
			}
			depth++
			if depth > 64 {
				return nil, errors.New("ComicInfo nesting capacity exceeded")
			}
			if depth == 2 && limits[value.Name.Local] > 0 {
				if seen[value.Name.Local] {
					return nil, errors.New("duplicate ComicInfo field")
				}
				field = value.Name.Local
				seen[field] = true
				text.Reset()
			} else if depth > 2 && field != "" {
				return nil, errors.New("ComicInfo field must contain plain text")
			}
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(value)) != "" {
				return nil, errors.New("text outside ComicInfo root")
			}
			if field != "" {
				if text.Len()+len(value) > limits[field] {
					return nil, errors.New("ComicInfo field capacity exceeded")
				}
				text.Write(value)
			}
		case xml.EndElement:
			if depth == 2 && field != "" {
				values[field] = strings.TrimSpace(text.String())
				field = ""
			}
			depth--
			if depth == 0 {
				rootClosed = true
			}
		case xml.Directive:
			return nil, errors.New("ComicInfo declarations/entities are not allowed")
		}
	}
	if !rootSeen || !rootClosed || depth != 0 {
		return nil, errors.New("incomplete ComicInfo document")
	}
	return values, nil
}
