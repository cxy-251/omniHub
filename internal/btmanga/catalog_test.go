package btmanga

import (
	"context"
	"testing"
)

func TestCoverURLRejectsOtherTargets(t *testing.T) {
	good := "https://lain.bgm.tv/r/400/pic/cover/l/67/31/27466_rQ7gG.jpg"
	if !ValidCoverURL(good) {
		t.Fatal("rejected catalog cover")
	}
	for _, address := range []string{
		"https://uploads.mangadex.org/covers/ff507ebe-8480-4f09-8ff8-cf648cd67313/28860fce-9e0c-479c-8102-7a4790e8beb1.jpg.256.jpg",
		"https://covers.openlibrary.org/b/id/535745-M.jpg?default=false",
	} {
		if !ValidCoverURL(address) {
			t.Fatalf("rejected supported cover: %s", address)
		}
	}
	for _, address := range []string{
		"http://lain.bgm.tv/pic/cover/l/67/31/27466.jpg",
		"https://lain.bgm.tv.evil.example/pic/cover/l/67/31/27466.jpg",
		"https://lain.bgm.tv@127.0.0.1/pic/cover/l/67/31/27466.jpg",
		"https://lain.bgm.tv:444/pic/cover/l/67/31/27466.jpg",
		"https://lain.bgm.tv/pic/cover/l/67/31/../../private.jpg",
		good + "?redirect=http://127.0.0.1",
		"https://uploads.mangadex.org/../../private",
		"https://uploads.mangadex.org.evil.example/covers/a/b.jpg",
		"https://covers.openlibrary.org/b/id/535745-M.jpg?default=false&redirect=http://127.0.0.1",
		"https://covers.openlibrary.org/works/OL5799436W.json",
	} {
		if ValidCoverURL(address) {
			t.Fatalf("accepted unexpected image target: %s", address)
		}
	}
}

func TestCoverRedirectStaysOnTheSameArchiveImage(t *testing.T) {
	original := "https://covers.openlibrary.org/b/id/8782810-M.jpg?default=false"
	archive := "https://archive.org/download/m_covers_0008/m_covers_0008_78.zip/0008782810-M.jpg"
	member := "https://ia800703.us.archive.org/view_archive.php?archive=/4/items/m_covers_0008/m_covers_0008_78.zip&file=0008782810-M.jpg"
	for _, address := range []string{archive, member} {
		if !validCoverRedirect(address, original) {
			t.Fatalf("cover redirect rejected: %s", address)
		}
	}
	for _, address := range []string{
		"http://127.0.0.1/private.jpg", archive + "?redirect=bad", archive + ".zip",
		"https://archive.org/download/private/secret.zip/file.jpg",
		"https://ia800703.us.archive.org/view_archive.php?archive=/etc/passwd&file=0008782810-M.jpg",
		member + "&file=secret.jpg", member + "&redirect=http://127.0.0.1",
		"https://ia800703.us.archive.org.evil.example/view_archive.php?archive=/4/items/m_covers_0008/m_covers_0008_78.zip&file=0008782810-M.jpg",
	} {
		if validCoverRedirect(address, original) {
			t.Fatalf("unsafe image redirect accepted: %s", address)
		}
	}
	if validCoverRedirect(archive, "https://covers.openlibrary.org/b/id/535745-M.jpg?default=false") {
		t.Fatal("redirect fetched a different cover ID")
	}
}

func TestAdditionalCatalogContentAndIdentityBoundaries(t *testing.T) {
	md := mangaDexRecord{ID: "ff507ebe-8480-4f09-8ff8-cf648cd67313"}
	md.Attributes.Title = map[string]string{"en": "Example"}
	md.Attributes.ContentRating = "safe"
	if _, ok := md.subject(); !ok {
		t.Fatal("ordinary MangaDex title rejected")
	}
	for _, rating := range []string{"erotica", "pornographic", ""} {
		md.Attributes.ContentRating = rating
		if _, ok := md.subject(); ok {
			t.Fatal("adult or unclassified MangaDex title exposed")
		}
	}
	ol := openLibraryWork{Key: "/works/OL5799436W", Title: "Example", Subjects: []string{"Comics & Graphic Novels"}}
	if _, ok := ol.subject(); !ok {
		t.Fatal("ordinary comic rejected")
	}
	for _, subjects := range [][]string{{"Comics", "Erotica"}, {"Forest conservation"}, nil} {
		ol.Subjects = subjects
		if _, ok := ol.subject(); ok {
			t.Fatal("adult or non-comic book exposed")
		}
	}
	for _, key := range []string{"file:etc/passwd", "mangadex:../../private", "openlibrary:../../private", "bangumi:-1", "openlibrary:OL5799436W?redirect=bad"} {
		if _, err := GetCatalogSubject(context.Background(), key, ""); err == nil {
			t.Fatalf("unexpected external catalog identity accepted: %s", key)
		}
	}
	for _, title := range []string{"The Tale of the Outcasts v01", "Welcome to the Outcast's Restaurant! v01"} {
		if matchesSelectedTitle(title, []string{"The Outcast"}) {
			t.Fatalf("unrelated title identified as selected work: %s", title)
		}
	}
	if !matchesSelectedTitle("[Group] The Outcast v01 (Digital)", []string{"The Outcast"}) {
		t.Fatal("selected work title rejected")
	}
	if !nonComicFormat.MatchString("The Outcast Writer [KR Web Novel]") {
		t.Fatal("novel incorrectly offered as manga")
	}
}

func TestCatalogRejectsAdultAndNonMangaSubjects(t *testing.T) {
	raw := catalogSubject{ID: 1, Type: 1, Name: "Example", Platform: "漫画"}
	if _, ok := raw.subject(); !ok {
		t.Fatal("ordinary manga rejected")
	}
	raw.NSFW = true
	if _, ok := raw.subject(); ok {
		t.Fatal("adult entry exposed")
	}
	raw.NSFW = false
	raw.Type = 2
	if _, ok := raw.subject(); ok {
		t.Fatal("anime entry exposed")
	}
	raw.Type = 1
	raw.Platform = "小说"
	if _, ok := raw.subject(); ok {
		t.Fatal("novel entry exposed")
	}
}
