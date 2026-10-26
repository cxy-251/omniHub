package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"omnihub/internal/library"
)

// 漫画按子目录分级：manga/nsfw/（默认仅管理员）与 manga/standard/（跟随「漫画」等级）。
// 以前的资源库把漫画平铺在 manga/ 下，那时全是 NSFW。
var mangaLayoutMu sync.Mutex

// migrateMangaLayout 把可写库 manga/ 下直接放着的 CBZ 搬进 manga/nsfw/（同一个盘上改名，不复制），
// 同时把索引 manga.db 与账号数据（阅读进度、点赞、看过、回收站）里的路径一起改掉，换了设备或插上别的库也会自己修正。
// nsfw/ 里已有同名文件的不搬，留在原处仍按 NSFW 对待（见 subsectionRoots 的 rootFiles）。
// 一般漫画要放进 manga/standard/：放在 manga/ 下也会被当成旧布局搬进 nsfw/。
func (r *Router) migrateMangaLayout(ctx context.Context, lib *library.Library, dir string) error {
	mangaLayoutMu.Lock()
	defer mangaLayoutMu.Unlock()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var loose []string
	for _, e := range entries {
		if e.Type().IsRegular() && !strings.HasPrefix(e.Name(), ".") && strings.EqualFold(filepath.Ext(e.Name()), ".cbz") {
			loose = append(loose, e.Name())
		}
	}
	if len(loose) == 0 {
		return nil
	}
	nsfw := filepath.Join(dir, "nsfw")
	if err := os.MkdirAll(nsfw, 0o755); err != nil {
		return err
	}
	moved := map[string]string{} // 旧逻辑路径 → 新逻辑路径
	var errs []error
	for _, name := range loose {
		to := filepath.Join(nsfw, name)
		if _, err := os.Lstat(to); err == nil || !errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err := os.Rename(filepath.Join(dir, name), to); err != nil {
			errs = append(errs, err)
			continue
		}
		moved[lib.ID+":media_library/manga/"+name] = lib.ID + ":media_library/manga/nsfw/" + name
	}
	r.mangaCatalog.Invalidate(dir)
	if len(moved) == 0 {
		return errors.Join(errs...)
	}
	if _, err := os.Stat(filepath.Join(dir, "manga.db")); err == nil {
		if db, err := r.getMangaDB(lib.RootPath); err != nil {
			errs = append(errs, err)
		} else {
			for from := range moved {
				name := strings.TrimPrefix(from, lib.ID+":media_library/manga/")
				if err := db.RenamePath(ctx, name, "nsfw/"+name); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	if r.userState != nil {
		if err := r.userState.RenameItems(ctx, "manga", moved); err != nil {
			errs = append(errs, fmt.Errorf("账号数据里的路径没改成：%w", err))
		}
	}
	log.Printf("manga layout %s: moved %d of %d loose CBZ into nsfw/", lib.Name, len(moved), len(loose))
	return errors.Join(errs...)
}
