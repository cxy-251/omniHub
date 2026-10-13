package manga

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"path"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/webp"
)

// A cover is decoded only on selection, under the shared media read budget.
// Config validation prevents small compressed inputs allocating enormous rasters.
const maxCoverPixels = 16 << 20

type coverInput struct {
	ctx context.Context
	*bytes.Reader
}

// Read stops a decoder promptly when its request is cancelled.
func (r coverInput) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

// ReadByte applies the same cancellation boundary to byte-oriented decoders.
func (r coverInput) ReadByte() (byte, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.ReadByte()
}

func validateCover(ctx context.Context, data []byte) error {
	config, _, err := image.DecodeConfig(coverInput{ctx, bytes.NewReader(data)})
	if err != nil {
		return err
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > maxCoverPixels/config.Height {
		return errors.New("cover image exceeds pixel capacity")
	}
	_, _, err = image.Decode(coverInput{ctx, bytes.NewReader(data)})
	return errors.Join(err, ctx.Err())
}

// ValidateCoverEntryContext rejects absent, unsupported or undecodable entries
// before persisting a custom cover; it never falls back to a different page.
func (r *CBZReader) ValidateCoverEntryContext(ctx context.Context, entry string) error {
	if len(entry) > 4096 || !ImageExtensions[strings.ToLower(filepath.Ext(entry))] {
		return errors.New("cover must be an image entry")
	}
	data, err := r.ReadEntryContext(ctx, entry)
	if err != nil {
		return err
	}
	return validateCover(ctx, data)
}

// ReadCoverContext tries a custom cover, one independent root cover, then at
// most three pages from the first chapter. It skips unreadable images and never
// scans later chapters. Successful selection retains one entry key, not pixels.
// GIF validation decodes its first frame; animation is returned unchanged.
func (r *CBZReader) ReadCoverContext(ctx context.Context, custom string) ([]byte, error) {
	if len(custom) > 4096 {
		return nil, errors.New("cover key exceeds capacity")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.coverMu.Lock()
	entry, key := r.coverEntry, r.coverKey
	r.coverMu.Unlock()
	if entry != "" && key == custom {
		return r.ReadEntryContext(ctx, entry)
	}
	candidates := make([]string, 0, 5)
	if r.GetCoverEntry(custom) == custom && custom != "" {
		candidates = append(candidates, custom)
	}
	for _, entry := range r.allImages {
		if !strings.Contains(entry, "/") && strings.HasPrefix(strings.ToLower(path.Base(entry)), "cover.") {
			candidates = append(candidates, entry)
			break
		}
	}
	for i, page := range r.pages {
		if i == 3 || page.ChapterIndex != 0 {
			break
		}
		entry, err := DecodeEntry(page.Entry)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, entry)
	}
	seen := make(map[string]bool, len(candidates))
	var failures error
	for _, entry := range candidates {
		if seen[entry] {
			continue
		}
		seen[entry] = true
		data, err := r.ReadEntryContext(ctx, entry)
		if err == nil {
			err = validateCover(ctx, data)
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err == nil {
			r.coverMu.Lock()
			r.coverKey, r.coverEntry = custom, entry
			r.coverMu.Unlock()
			return data, nil
		}
		failures = errors.Join(failures, err)
	}
	return nil, errors.Join(errors.New("no usable cover in bounded candidates"), failures)
}
