package games

import (
	"context"
	"testing"
)

func TestSnapshotRollsBackWholeBatchAndMissingMarksOnSQLFailure(t *testing.T) {
	db, err := OpenDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	original := &Game{ID: "001", Name: "Original", Category: "unity", Target: "gui_only", PlayTimeSec: 99}
	if err := db.UpsertGame(original); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`CREATE TRIGGER fail_second BEFORE INSERT ON games WHEN NEW.id='002' BEGIN SELECT RAISE(ABORT,'injected write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := db.PublishScan(context.Background(), []*Game{{ID: "001", Name: "Changed", Category: "unity", Target: "gui_only"}, {ID: "002", Name: "New", Category: "unity", Target: "gui_only"}}); err == nil {
		t.Fatal("failed snapshot committed")
	}
	value, err := db.GetGame(context.Background(), "001")
	if err != nil || value.Name != "Original" || !value.Available || value.PlayTimeSec != 99 {
		t.Fatalf("batch partially published: %+v %v", value, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := db.PublishScan(ctx, nil); err == nil {
		t.Fatal("cancelled transaction published missing marks")
	}
	value, err = db.GetGame(context.Background(), "001")
	if err != nil || !value.Available {
		t.Fatal("cancelled snapshot marked game missing")
	}
}

func TestScopedSnapshotLeavesDisabledCategoryHistoryUntouched(t *testing.T) {
	db, err := OpenDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.UpsertGames([]*Game{{ID: "001", Name: "Active", Category: "unity", Target: "gui_only"}, {ID: "009", Name: "Disabled", Category: "godot", Target: "gui_only", PlayTimeSec: 55}}); err != nil {
		t.Fatal(err)
	}
	if err := db.PublishCategories(context.Background(), nil, []string{"unity"}); err != nil {
		t.Fatal(err)
	}
	active, err := db.GetGame(context.Background(), "001")
	if err != nil || active.Available {
		t.Fatal("absent active-category game not reconciled")
	}
	disabled, err := db.GetGame(context.Background(), "009")
	if err != nil || !disabled.Available || disabled.PlayTimeSec != 55 {
		t.Fatal("unscanned disabled category was modified")
	}
}
