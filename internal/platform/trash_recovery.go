package platform

import (
	"bufio"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// TrashReceipt identifies one OS recycle-bin entry; never accept it from a client.
type TrashReceipt struct {
	Original  string `json:"original"`
	Directory string `json:"directory"`
	Name      string `json:"name"`
}

type trashLocation struct{ dir, base string }

func trashLocations(original string) []trashLocation {
	data := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(data) {
		home, _ := os.UserHomeDir()
		data = filepath.Join(home, ".local/share")
	}
	out := []trashLocation{{filepath.Join(data, "Trash"), data}}
	uid := strconv.Itoa(os.Getuid())
	for dir := filepath.Dir(original); ; dir = filepath.Dir(dir) {
		out = append(out, trashLocation{filepath.Join(dir, ".Trash-"+uid), dir}, trashLocation{filepath.Join(dir, ".Trash", uid), dir})
		if dir == filepath.Dir(dir) {
			break
		}
	}
	return out
}

func trashOriginal(info, base string) (string, error) {
	f, err := os.Open(info)
	if err != nil {
		return "", err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 1024), 65536)
	if !s.Scan() || s.Text() != "[Trash Info]" {
		return "", errors.New("invalid recycle-bin metadata")
	}
	for s.Scan() {
		if value, ok := strings.CutPrefix(s.Text(), "Path="); ok {
			p, err := url.PathUnescape(value)
			if err != nil {
				return "", err
			}
			if !filepath.IsAbs(p) {
				if !filepath.IsLocal(p) {
					return "", errors.New("invalid recycle-bin original path")
				}
				p = filepath.Join(base, p)
			}
			return filepath.Clean(p), nil
		}
	}
	return "", errors.New("recycle-bin original path missing")
}

// TrashWithReceipt uses the existing OS trash operation, then finds only the
// newly created entry whose original path and inode match the selected file.
// A non-empty Original means the file moved even if receipt lookup reports an error.
func TrashWithReceipt(path string) (TrashReceipt, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return TrashReceipt{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return TrashReceipt{}, err
	}
	if !info.Mode().IsRegular() {
		return TrashReceipt{}, errors.New("only regular files can be recycled here")
	}
	locations := trashLocations(path)
	before := map[string]map[string]bool{}
	for _, loc := range locations {
		entries, _ := os.ReadDir(filepath.Join(loc.dir, "info"))
		names := map[string]bool{}
		for _, e := range entries {
			names[e.Name()] = true
		}
		before[loc.dir] = names
	}
	if err := Trash(path); err != nil {
		return TrashReceipt{}, err
	}
	for _, loc := range locations {
		entries, _ := os.ReadDir(filepath.Join(loc.dir, "info"))
		for _, e := range entries {
			if before[loc.dir][e.Name()] || !strings.HasSuffix(e.Name(), ".trashinfo") || e.Type()&os.ModeSymlink != 0 {
				continue
			}
			original, err := trashOriginal(filepath.Join(loc.dir, "info", e.Name()), loc.base)
			if err != nil || original != path {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".trashinfo")
			moved, err := os.Lstat(filepath.Join(loc.dir, "files", name))
			if err == nil && moved.Mode().IsRegular() && os.SameFile(info, moved) {
				return TrashReceipt{path, loc.dir, name}, nil
			}
		}
	}
	return TrashReceipt{Original: path}, errors.New("已移入系统回收站，未能读取恢复凭据；可从系统回收站恢复")
}

func validateTrashReceipt(receipt TrashReceipt) error {
	if !filepath.IsAbs(receipt.Original) || receipt.Name == "" || filepath.Base(receipt.Name) != receipt.Name || receipt.Name == "." || receipt.Name == ".." {
		return errors.New("invalid recycle-bin receipt")
	}
	for _, loc := range trashLocations(receipt.Original) {
		if filepath.Clean(loc.dir) != filepath.Clean(receipt.Directory) {
			continue
		}
		original, err := trashOriginal(filepath.Join(loc.dir, "info", receipt.Name+".trashinfo"), loc.base)
		if err == nil && original == receipt.Original {
			return nil
		}
	}
	return errors.New("回收条目已改变或已从系统回收站恢复")
}
