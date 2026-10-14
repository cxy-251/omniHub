package auth

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestAccountCredentialChangesInvalidateProofs(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	owner, err := db.CreateInitialAdmin("owner", "original")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CreateSession(owner.ID, "enroll", time.Hour); err != nil {
		t.Fatal(err)
	}
	_, device, err := db.RegisterDevice(ctx, owner.ID, HashToken("enroll"), "tablet", "192.168.1.2")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CreateSession(owner.ID, "normal", time.Hour); err != nil {
		t.Fatal(err)
	}
	proof, err := db.Authenticate("one", "owner", "original")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SetPassword(ctx, owner.ID, "replacement"); err != nil {
		t.Fatal(err)
	}
	if user, err := db.GetUserByToken("normal"); err != nil || user != nil {
		t.Fatal("old session survived", err)
	}
	if user, _, err := db.TrustedUserByHash(ctx, HashToken(device)); err != nil || user != nil {
		t.Fatal("old device survived", err)
	}
	if err = db.CreateVerifiedSession(ctx, proof, "late", time.Hour); !errors.Is(err, ErrCredentialChanged) {
		t.Fatal("stale login accepted", err)
	}
	if err = db.SetVerifiedPassword(ctx, proof, "stale-change"); !errors.Is(err, ErrCredentialChanged) {
		t.Fatal("stale password change accepted", err)
	}
	if _, err = db.Authenticate("two", "owner", "original"); err == nil {
		t.Fatal("old password accepted")
	}
	fresh, err := db.Authenticate("three", "owner", "replacement")
	if err != nil || fresh.ID != owner.ID {
		t.Fatal("replacement lost identity", err)
	}
	if err = db.CreateVerifiedSession(ctx, fresh, "fresh", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err = db.SetUserStatus(ctx, owner.ID, "active"); err != nil {
		t.Fatal(err)
	}
	if u, err := db.GetUserByToken("fresh"); err != nil || u == nil {
		t.Fatal("idempotent status revoked session", err)
	}
	if err = db.UpdateUserLevel(owner.ID, LevelAdmin); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateVerifiedSession(ctx, fresh, "late-level", time.Hour); !errors.Is(err, ErrCredentialChanged) {
		t.Fatal("stale level proof accepted", err)
	}
}

func TestLastAdminAndDisableRestoreProof(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	first, err := db.CreateInitialAdmin("first", "password")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SetUserStatus(ctx, first.ID, "disabled"); !errors.Is(err, ErrLastAdmin) {
		t.Fatal(err)
	}
	second, err := db.CreateUser("second", "password", RoleAdmin, LevelAdmin)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := db.Authenticate("proof", "first", "password")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []int64{first.ID, second.ID} {
		wg.Add(1)
		go func(id int64) { defer wg.Done(); results <- db.SetUserStatus(ctx, id, "disabled") }(id)
	}
	wg.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if !errors.Is(err, ErrLastAdmin) {
			t.Fatal(err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("disabled %d admins", succeeded)
	}
	var active int
	if err = db.db.QueryRow("SELECT COUNT(*) FROM users WHERE role='admin' AND status='active'").Scan(&active); err != nil || active != 1 {
		t.Fatal("last admin lost", err)
	}
	// Ensure first experiences a complete disable/restore cycle irrespective of the winner.
	if err = db.SetUserStatus(ctx, second.ID, "active"); err != nil {
		t.Fatal(err)
	}
	if err = db.SetUserStatus(ctx, first.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Authenticate("disabled", "first", "password"); err == nil {
		t.Fatal("disabled login accepted")
	}
	if err = db.SetUserStatus(ctx, first.ID, "active"); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateVerifiedSession(ctx, proof, "late-restore", time.Hour); !errors.Is(err, ErrCredentialChanged) {
		t.Fatal("disable/restore resurrected proof", err)
	}
	users, err := db.ListUsersPage(ctx, 1, 1)
	if err != nil || len(users) != 1 || users[0].ID != second.ID || users[0].PasswordHash != "" {
		t.Fatal("unsafe account pagination", err)
	}
}

func TestLegacyAccountsGainCredentialVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := db.CreateInitialAdmin("legacy", "password")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CreateSession(owner.ID, "legacy-session", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err = db.db.Exec("ALTER TABLE users DROP COLUMN auth_version"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user, err := db.Authenticate("migration", "legacy", "password")
	if err != nil || user.ID != owner.ID || user.AuthVersion != 0 {
		t.Fatal("migration changed account", err)
	}
	if session, err := db.GetUserByToken("legacy-session"); err != nil || session == nil {
		t.Fatal("migration invalidated existing session", err)
	}
	if err = db.SetVerifiedPassword(context.Background(), user, "replacement"); err != nil {
		t.Fatal(err)
	}
	if session, err := db.GetUserByToken("legacy-session"); err != nil || session != nil {
		t.Fatal("migrated account reset left session", err)
	}
}
