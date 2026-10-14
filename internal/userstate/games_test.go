package userstate

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestGameStatisticsAccumulateFractionsAndIsolateAccounts(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "user_state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	start := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	for _, moment := range []time.Time{start.Add(500 * time.Millisecond), start} {
		if err := db.RecordGamePlay(ctx, 1, "001", moment, moment.Add(600*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := db.GameStats(ctx, 1, []string{"001", "missing"})
	if err != nil || len(stats) != 1 || stats["001"].PlayTimeSec != 1 || !stats["001"].LastPlayedAt.Equal(start.Add(500*time.Millisecond)) {
		t.Fatalf("aggregate/last start: %+v %v", stats, err)
	}
	for _, user := range []int64{0, 2} {
		stats, err := db.GameStats(ctx, user, []string{"001"})
		if err != nil || len(stats) != 0 {
			t.Fatalf("account leak: %d %+v %v", user, stats, err)
		}
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if err := db.RecordGamePlay(cancelCtx, 1, "001", start, start.Add(time.Hour)); err == nil {
		t.Fatal("cancelled observation persisted")
	}
	if err := db.RecordGamePlay(ctx, 0, "001", start, start); !errors.Is(err, ErrInvalidGamePlay) {
		t.Fatal("guest accepted")
	}
	if err := db.RecordGamePlay(ctx, 1, "001", start, start.Add(-time.Second)); !errors.Is(err, ErrInvalidGamePlay) {
		t.Fatal("negative duration accepted")
	}
	if _, err := db.GameStats(ctx, 1, make([]string, 101)); !errors.Is(err, ErrInvalidGamePlay) {
		t.Fatal("unbounded page accepted")
	}
	if _, err := db.db.Exec("UPDATE user_game_stats SET play_time_ms=?", int64(math.MaxInt64)); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordGamePlay(ctx, 1, "001", start, start.Add(time.Second)); err == nil {
		t.Fatal("counter overflow accepted")
	}
	var milliseconds int64
	if err := db.db.QueryRow("SELECT play_time_ms FROM user_game_stats WHERE user_id=1 AND game_id='001'").Scan(&milliseconds); err != nil || milliseconds != math.MaxInt64 {
		t.Fatal("failed observation changed counter", err)
	}
}
