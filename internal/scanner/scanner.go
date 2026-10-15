package scanner

import "context"

// ScanManga 扫描 media_library/manga 目录下的 CBZ 文件并建立索引。
func ScanManga(mangaDir string) (int, error) {
	return ScanMangaContext(context.Background(), mangaDir, mangaDir)
}
