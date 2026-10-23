package server

import (
	"context"
	"errors"
	"omnihub/internal/games"
	"omnihub/internal/userstate"
	"strings"
	"time"
)

// Capture this generation's state database; runtime admission prevents migration
// until the completion callback has finished. No physical game library is written.
func gamePlayRecorder(db *userstate.DB) func(games.RunStatus) error {
	return func(run games.RunStatus) error {
		_, id, ok := strings.Cut(run.Key, ":")
		if !ok {
			return errors.New("game session lacks indexed identity")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.RecordGamePlay(ctx, run.UserID, id, run.StartedAt, run.EndedAt); err != nil {
			return errors.New("游玩时长保存失败：" + err.Error())
		}
		if p := run.Profile; p != nil {
			if err := db.RecordGameProfile(ctx, id, p.PeakMemMB, p.PeakIOMBps, p.AvgCPUPct, p.Seconds); err != nil {
				return errors.New("硬件画像保存失败：" + err.Error())
			}
		}
		return nil
	}
}
