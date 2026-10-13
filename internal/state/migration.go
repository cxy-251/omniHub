// Package state 实现全局状态迁移与崩溃恢复，模块数据库不参与迁移。
package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"omnihub/internal/config"
	"omnihub/internal/platform"
	"omnihub/internal/storage"
)

// Plan 描述已经由用户确认的迁移；Bootstrap 表示源目录同时含有应用配置，不能整目录移动。
type Plan struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Bootstrap   bool   `json:"bootstrap"`
}

type fileDigest struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type journal struct {
	Plan
	Mode      string       `json:"mode"`
	Temporary string       `json:"temporary,omitempty"`
	Files     []fileDigest `json:"files"`
}

// Migrate 在全局数据库已关闭后迁移状态并调用 commit 持久化配置。
// 同文件系统整目录排他重命名；跨文件系统或旧初始化目录流式复制并逐文件校验。
// journalPath 必须位于始终在线的应用配置目录；失败保留源数据并回滚目标位置。
func Migrate(journalPath string, plan Plan, commit func() error) error {
	if err := validatePlan(plan); err != nil {
		return err
	}
	if _, err := os.Stat(journalPath); err == nil {
		return errors.New("unfinished state migration requires recovery")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	files, err := inspect(plan)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return errors.New("global state has no database files")
	}
	if err := prepareDestination(plan.Destination); err != nil {
		return err
	}
	j := journal{Plan: plan, Mode: "rename", Files: files}
	if plan.Bootstrap {
		j.Mode = "copy"
	}
	if err := saveJournal(journalPath, j); err != nil {
		return err
	}
	if j.Mode == "rename" {
		err = platform.RenameNoReplace(plan.Source, plan.Destination)
		if err != nil {
			// 只在确实未移动源目录且目标仍不存在时尝试复制；目标存在绝不覆盖。
			if _, sourceErr := os.Stat(plan.Source); sourceErr != nil {
				return errors.Join(err, sourceErr)
			}
			if _, targetErr := os.Lstat(plan.Destination); !errors.Is(targetErr, os.ErrNotExist) {
				return errors.Join(err, targetErr)
			}
			j.Mode = "copy"
			if err := saveJournal(journalPath, j); err != nil {
				return err
			}
		}
	}
	if j.Mode == "copy" {
		if err := copyState(journalPath, &j); err != nil {
			return errors.Join(err, Recover(journalPath, plan.Source))
		}
	}
	if err := platform.SyncDirectory(filepath.Dir(plan.Destination)); err != nil {
		return errors.Join(err, Recover(journalPath, plan.Source))
	}
	if j.Mode == "rename" {
		if err := platform.SyncDirectory(filepath.Dir(plan.Source)); err != nil {
			return errors.Join(err, Recover(journalPath, plan.Source))
		}
	}
	if err := verify(plan.Destination, files); err != nil {
		return errors.Join(err, Recover(journalPath, plan.Source))
	}
	if err := commit(); err != nil {
		if config.WriteWasCommitted(err) {
			return errors.Join(err, removeJournal(journalPath))
		}
		return errors.Join(err, Recover(journalPath, plan.Source))
	}
	return removeJournal(journalPath)
}

// Recover 根据已持久化的状态位置恢复未完成迁移。复制后的旧副本不自动删除。
// 未提交的完整目标回到记录的暂存目录，供用户明确清理；原账号库保持原位置。
func Recover(journalPath, configuredState string) error {
	data, err := os.ReadFile(journalPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(data) > 2<<20 {
		return errors.New("migration journal exceeds size limit")
	}
	var j journal
	if err := json.Unmarshal(data, &j); err != nil {
		return fmt.Errorf("parse migration recovery journal: %w", err)
	}
	if err := validatePlan(j.Plan); err != nil {
		return err
	}
	if configuredState == j.Destination {
		// 提交后数据库可能已有正常新写入，不能要求仍等于迁移时的旧哈希。
		info, err := os.Lstat(filepath.Join(j.Destination, "users.db"))
		if err != nil {
			return fmt.Errorf("committed state unavailable: %w", err)
		}
		if !info.Mode().IsRegular() {
			return errors.New("committed user database is not a regular file")
		}
		return removeJournal(journalPath)
	}
	if configuredState != j.Source {
		return errors.New("configuration conflicts with migration recovery journal")
	}
	if _, err := os.Lstat(j.Destination); errors.Is(err, os.ErrNotExist) {
		return removeJournal(journalPath)
	} else if err != nil {
		return err
	}
	if err := verify(j.Destination, j.Files); err != nil {
		return fmt.Errorf("refuse to move changed recovery destination: %w", err)
	}
	switch j.Mode {
	case "rename":
		if err := platform.RenameNoReplace(j.Destination, j.Source); err != nil {
			return fmt.Errorf("restore original state location: %w", err)
		}
	case "copy":
		if j.Temporary == "" || filepath.Dir(j.Temporary) != filepath.Dir(j.Destination) || !strings.HasPrefix(filepath.Base(j.Temporary), ".omni-migration-") {
			return errors.New("invalid migration temporary location")
		}
		if err := platform.RenameNoReplace(j.Destination, j.Temporary); err != nil {
			return fmt.Errorf("preserve uncommitted state copy: %w", err)
		}
	default:
		return errors.New("invalid migration recovery mode")
	}
	return removeJournal(journalPath)
}

func validatePlan(plan Plan) error {
	if !filepath.IsAbs(plan.Source) || !filepath.IsAbs(plan.Destination) {
		return errors.New("state migration paths must be absolute")
	}
	for _, pair := range [][2]string{{plan.Source, plan.Destination}, {plan.Destination, plan.Source}} {
		rel, err := filepath.Rel(pair[0], pair[1])
		if err == nil && (rel == "." || filepath.IsLocal(rel)) {
			return errors.New("state migration locations must not overlap")
		}
	}
	return nil
}

func prepareDestination(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("migration destination must be a physical directory")
	}
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("target primary already contains global state; refusing to overwrite")
	}
	// 仅移除空的状态容器；若别的进程写入任何内容，Remove 会失败。
	return os.Remove(path)
}

func inspect(plan Plan) ([]fileDigest, error) {
	info, err := os.Lstat(plan.Source)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("state source must be a physical directory")
	}
	var files []fileDigest
	err = filepath.WalkDir(plan.Source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(plan.Source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if plan.Bootstrap && rel != "users.db" && rel != "audio_progress.db" && rel != "user_state.db" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			if rel == "users.db-wal" || rel == "audio_progress.db-wal" || rel == "user_state.db-wal" {
				info, err := entry.Info()
				if err != nil {
					return err
				}
				if info.Size() != 0 {
					return errors.New("global database WAL not checkpointed")
				}
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("global state contains symbolic link")
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return errors.New("global state contains non-regular file")
		}
		if len(files) >= 4096 || len(rel) > 4096 {
			return errors.New("global state manifest exceeds migration limits")
		}
		if strings.HasSuffix(rel, ".db-wal") {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Size() != 0 {
				return errors.New("global database WAL not checkpointed")
			}
		}
		if strings.HasSuffix(rel, ".db") {
			if err := checkDatabase(path); err != nil {
				return err
			}
		}
		digest, err := hashFile(path)
		if err != nil {
			return err
		}
		digest.Path = rel
		files = append(files, digest)
		return nil
	})
	foundUsers := false
	for _, file := range files {
		foundUsers = foundUsers || file.Path == "users.db"
	}
	if err == nil && !foundUsers {
		return nil, errors.New("state migration requires the existing users database")
	}
	return files, err
}

func checkDatabase(path string) (result error) {
	db, err := storage.OpenSQLite(path, true)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, db.Close()) }()
	var status string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&status); err != nil {
		return err
	}
	if status != "ok" {
		return errors.New("global database integrity check failed")
	}
	return nil
}

func hashFile(path string) (digest fileDigest, result error) {
	info, err := os.Lstat(path)
	if err != nil {
		return digest, err
	}
	if !info.Mode().IsRegular() {
		return digest, errors.New("state file is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return digest, err
	}
	defer func() { result = errors.Join(result, f.Close()) }()
	opened, err := f.Stat()
	if err != nil {
		return digest, err
	}
	if !os.SameFile(info, opened) {
		return digest, errors.New("state file changed during verification")
	}
	hash := sha256.New()
	digest.Size, err = io.Copy(hash, f)
	if err != nil {
		return digest, err
	}
	digest.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return digest, nil
}

func verify(root string, files []fileDigest) error {
	if len(files) == 0 || len(files) > 4096 {
		return errors.New("invalid state migration manifest")
	}
	for _, expected := range files {
		if !filepath.IsLocal(expected.Path) || len(expected.Path) > 4096 {
			return errors.New("invalid state manifest path")
		}
		actual, err := hashFile(filepath.Join(root, expected.Path))
		if err != nil {
			return err
		}
		if actual.Size != expected.Size || actual.SHA256 != expected.SHA256 {
			return errors.New("state copy verification failed")
		}
	}
	return nil
}

func copyState(journalPath string, j *journal) error {
	temporary, err := os.MkdirTemp(filepath.Dir(j.Destination), ".omni-migration-")
	if err != nil {
		return err
	}
	j.Temporary = temporary
	if err := saveJournal(journalPath, *j); err != nil {
		return err
	}
	for _, file := range j.Files {
		path := filepath.Join(temporary, file.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		if err := copyFile(filepath.Join(j.Source, file.Path), path); err != nil {
			return err
		}
	}
	if err := verify(temporary, j.Files); err != nil {
		return err
	}
	return platform.RenameNoReplace(temporary, j.Destination)
}

func copyFile(source, destination string) (result error) {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, in.Close()) }()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, out.Close()) }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	return platform.SyncDirectory(filepath.Dir(destination))
}

func saveJournal(path string, j journal) error {
	data, err := json.Marshal(j)
	if err != nil {
		return err
	}
	return config.WritePrivateFile(path, data)
}

func removeJournal(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return platform.SyncDirectory(filepath.Dir(path))
}
