package userstate

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeResumeRevisionsAccountsRetentionAndNoopWrites(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "user_state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.db.SetMaxOpenConns(1)
	ctx := context.Background()
	p := NativePosition{ID: strings.Repeat("a", 64), Version: strings.Repeat("b", 64), Position: 42.5, Duration: 90, Paused: true, Volume: 42, Rate: 1.5}
	if err := db.SaveNative(ctx, 1, p); err != nil {
		t.Fatal(err)
	}
	var before, after int
	if err := db.db.QueryRow("SELECT total_changes()").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveNative(ctx, 1, p); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRow("SELECT total_changes()").Scan(&after); err != nil || before != after {
		t.Fatalf("identical native state rewrote database: %d %d %v", before, after, err)
	}
	for _, user := range []int64{2, 3} {
		got, err := db.Native(ctx, user, p.ID, p.Version)
		if err != nil || got != nil {
			t.Fatal("native account leak", got, err)
		}
	}
	old := p
	p.Version = strings.Repeat("c", 64)
	p.Position = 1
	if err := db.SaveNative(ctx, 1, p); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveNative(ctx, 1, old); err != nil {
		t.Fatal(err)
	}
	got, err := db.Native(ctx, 1, p.ID, p.Version)
	if err != nil || got.Position != 1 {
		t.Fatal("late old revision overwrote new state", got, err)
	}
	for _, position := range []float64{-1, 91, math.NaN(), math.Inf(1)} {
		bad := p
		bad.Position = position
		if err := db.SaveNative(ctx, 1, bad); err == nil {
			t.Fatal("invalid native cursor accepted")
		}
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	p.Position = 2
	if err := db.SaveNative(cancelCtx, 1, p); err == nil {
		t.Fatal("cancelled save persisted")
	}
	for i := 0; i < 260; i++ {
		p.ID = fmt.Sprintf("%064x", i)
		if err := db.SaveNative(ctx, 1, p); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.db.QueryRow("SELECT COUNT(*) FROM native_progress WHERE user_id=1").Scan(&count); err != nil || count != 256 {
		t.Fatal("native history unbounded", count, err)
	}
}
