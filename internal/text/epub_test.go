package text

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEpub(t *testing.T, packageXML string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "book.epub")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	entries := map[string]string{
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OPS/package.opf"/></rootfiles></container>`,
		"OPS/package.opf":        packageXML,
		"OPS/ch one.xhtml":       "<html><body>First</body></html>",
		"OPS/next.xhtml":         "<html><body>Second</body></html>",
	}
	for name, body := range entries {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

const samplePackage = `<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>A &amp; B</dc:title><dc:creator>Author</dc:creator></metadata><manifest>
<item id="second" href="next.xhtml" media-type="application/xhtml+xml"/>
<item id="first" href="ch%20one.xhtml" media-type="application/xhtml+xml"/>
</manifest><spine><itemref idref="first"/><itemref idref="second"/></spine></package>`

func TestEpubSpineUsesManifestAndDecodedRelativePaths(t *testing.T) {
	book, err := InspectEpub(context.Background(), writeEpub(t, samplePackage))
	if err != nil {
		t.Fatal(err)
	}
	if book.Title != "A & B" || book.Author != "Author" || len(book.Chapters) != 2 || book.Chapters[0].Entry != "OPS/ch one.xhtml" || book.Chapters[1].ID != "second" {
		t.Fatalf("wrong metadata or spine order: %+v", book)
	}
}

func TestEpubRejectsMissingOrEscapingSpineReferences(t *testing.T) {
	for _, replacement := range []string{"../../escape.xhtml", "https://example.invalid/first.xhtml", "missing.xhtml"} {
		t.Run(replacement, func(t *testing.T) {
			p := writeEpub(t, strings.Replace(samplePackage, "ch%20one.xhtml", replacement, 1))
			if _, err := InspectEpub(context.Background(), p); err == nil {
				t.Fatal("invalid chapter reference accepted")
			}
		})
	}
}
