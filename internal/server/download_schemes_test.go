package server

import (
	"reflect"
	"testing"
)

func TestMusicFilterArgsAndDefaults(t *testing.T) {
	r := &Router{}
	var music *dlKind
	for _, k := range r.schemeKinds() {
		if k.ID == "music" {
			music = k
			break
		}
	}
	if music == nil {
		t.Fatal("missing music scheme")
	}
	defaults := map[string]any{}
	for _, f := range music.Fields {
		switch f.Name {
		case "min_views", "min_replay", "filter_views", "filter_replay", "exempt_albums":
			defaults[f.Name] = f.Default
		}
	}
	if defaults["min_views"] != "1000000" || defaults["min_replay"] != "5.0" ||
		defaults["filter_views"] != true || defaults["filter_replay"] != true || defaults["exempt_albums"] != true {
		t.Fatalf("unexpected defaults: %+v", defaults)
	}

	args, err := musicFilterArgs("music", dlParams{
		"filter_views":  true,
		"min_views":     "1000000",
		"filter_replay": true,
		"min_replay":    "5.0",
		"exempt_albums": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--min-views", "1000000", "--min-replay", "5.0"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("got %v, want %v", args, want)
	}

	args, err = musicFilterArgs("music", dlParams{
		"filter_views":  false,
		"min_views":     "1000000",
		"filter_replay": false,
		"min_replay":    "5.0",
		"exempt_albums": false,
	})
	if err != nil || !reflect.DeepEqual(args, []string{"--no-exempt-albums"}) {
		t.Fatalf("unexpected disabled args: %v, err=%v", args, err)
	}

	args, err = musicFilterArgs("music", dlParams{"min_views": "", "min_replay": "0"})
	if err != nil || len(args) != 0 {
		t.Fatalf("expected no args when cancelled, got %v, err=%v", args, err)
	}

	if _, err := musicFilterArgs("music", dlParams{"min_views": "-1"}); err == nil {
		t.Fatal("expected error for negative min_views")
	}
	if _, err := musicFilterArgs("music", dlParams{"min_replay": "abc"}); err == nil {
		t.Fatal("expected error for invalid min_replay")
	}
}
