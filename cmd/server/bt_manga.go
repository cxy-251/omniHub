package main

import (
	"context"
	"encoding/json"
	"omnihub/internal/btmanga"
	"os"
	"os/signal"
	"syscall"
)

func runBTManga(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var plan btmanga.Plan
	if err = json.Unmarshal(data, &plan); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return btmanga.Run(ctx, plan, os.Stdout)
}
