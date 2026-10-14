package games

import (
	"context"
	"strings"
	"testing"
)

func TestCatalogOmitsLaunchFieldsAndRejectsOversizedDisplayData(t *testing.T) {
	db, err := OpenDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.UpsertGame(&Game{ID: "001", Name: "Native", Category: "unity", Target: "gui_only"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("UPDATE games SET args=?,env=?", strings.Repeat("a", 1<<20), strings.Repeat("e", 1<<20)); err != nil {
		t.Fatal(err)
	}
	list, err := db.ListCatalogPage(context.Background(), "gui_only", "", "", 32)
	if err != nil || len(list) != 1 || list[0].Args != "" || list[0].Env != "" {
		t.Fatalf("catalog loaded launch-only fields: %+v %v", list, err)
	}
	if _, err := db.GetGame(context.Background(), "001"); err == nil {
		t.Fatal("single-record launch lookup loaded oversized argument fields")
	}
	if _, err := db.db.Exec("UPDATE games SET name=?", strings.Repeat("中", 2000)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ListCatalogPage(context.Background(), "gui_only", "", "", 32); err == nil {
		t.Fatal("overlong UTF-8 display text accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := db.ListCatalogPage(ctx, "gui_only", "", "", 32); err == nil {
		t.Fatal("cancelled catalog query continued")
	}
}
