package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PDFDocInfo 描述工作区中一个 PDF 文档的元信息。
type PDFDocInfo struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	SizeBytes    int64   `json:"size_bytes"`
	Pages        int     `json:"pages"`
	PageWidthPt  float64 `json:"page_width_pt"`
	PageHeightPt float64 `json:"page_height_pt"`
	Title        string  `json:"title,omitempty"`
	Author       string  `json:"author,omitempty"`
	Encrypted    bool    `json:"encrypted"`
	CreatedAt    string  `json:"created_at"`
}

// PDFPageSpec 表示重排/合并/旋转/提取时的单页指令。
type PDFPageSpec struct {
	DocID  string `json:"doc_id"`
	Page   int    `json:"page"`   // 1-based page number in source DocID
	Rotate int    `json:"rotate"` // relative rotation: 0, 90, 180, 270
}

type pdfEntry struct {
	info      PDFDocInfo
	path      string
	createdAt time.Time
}

// PDFWorkspace 管理本地临时 PDF 编辑会话文件。
type PDFWorkspace struct {
	mu      sync.Mutex
	rootDir string
	docs    map[string]*pdfEntry
}

func NewPDFWorkspace(rootDir string) *PDFWorkspace {
	if rootDir == "" {
		rootDir = filepath.Join(os.TempDir(), "omnihub-pdf-workspace")
	}
	_ = os.MkdirAll(rootDir, 0o700)
	return &PDFWorkspace{
		rootDir: rootDir,
		docs:    make(map[string]*pdfEntry),
	}
}

func newPDFID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "pdf_" + hex.EncodeToString(b[:])
}

func sanitizePDFName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." || name == "/" {
		name = "document.pdf"
	}
	if !strings.HasSuffix(strings.ToLower(name), ".pdf") {
		name += ".pdf"
	}
	return name
}

func (w *PDFWorkspace) cleanupExpiredLocked() {
	cutoff := time.Now().Add(-2 * time.Hour)
	for id, e := range w.docs {
		if e.createdAt.Before(cutoff) {
			_ = os.Remove(e.path)
			delete(w.docs, id)
		}
	}
}

// List 返回当前工作区中的所有 PDF 文档（按创建时间倒序）。
func (w *PDFWorkspace) List() []PDFDocInfo {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cleanupExpiredLocked()
	out := make([]PDFDocInfo, 0, len(w.docs))
	for _, e := range w.docs {
		out = append(out, e.info)
	}
	return out
}

// GetPath 获取指定 ID 的本地文件路径与元信息。
func (w *PDFWorkspace) GetPath(id string) (string, PDFDocInfo, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cleanupExpiredLocked()
	e, ok := w.docs[id]
	if !ok {
		return "", PDFDocInfo{}, errors.New("找不到该 PDF 文档或会话已过期")
	}
	return e.path, e.info, nil
}

// Delete 删除工作区中的指定 PDF。
func (w *PDFWorkspace) Delete(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	e, ok := w.docs[id]
	if !ok {
		return false
	}
	_ = os.Remove(e.path)
	delete(w.docs, id)
	return true
}

// RegisterFile 将已写入工作区目录的临时 PDF 解析元信息并注册。
func (w *PDFWorkspace) RegisterFile(tmpPath, displayName, password string) (PDFDocInfo, error) {
	st, err := os.Stat(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return PDFDocInfo{}, err
	}
	info, err := inspectPDFFile(tmpPath, password)
	if err != nil {
		_ = os.Remove(tmpPath)
		return PDFDocInfo{}, err
	}
	id := newPDFID()
	finalPath := filepath.Join(w.rootDir, id+".pdf")
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return PDFDocInfo{}, err
	}

	now := time.Now()
	info.ID = id
	info.Name = sanitizePDFName(displayName)
	info.SizeBytes = st.Size()
	info.CreatedAt = now.Format("15:04:05")

	w.mu.Lock()
	w.cleanupExpiredLocked()
	w.docs[id] = &pdfEntry{
		info:      info,
		path:      finalPath,
		createdAt: now,
	}
	w.mu.Unlock()
	return info, nil
}

// ImportReader 从上传流或文件流导入 PDF。
func (w *PDFWorkspace) ImportReader(name string, r io.Reader, maxBytes int64) (PDFDocInfo, error) {
	if err := os.MkdirAll(w.rootDir, 0o700); err != nil {
		return PDFDocInfo{}, err
	}
	tmp, err := os.CreateTemp(w.rootDir, "import-*.pdf")
	if err != nil {
		return PDFDocInfo{}, err
	}
	tmpPath := tmp.Name()
	lr := &io.LimitedReader{R: r, N: maxBytes + 1}
	n, copyErr := io.Copy(tmp, lr)
	_ = tmp.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return PDFDocInfo{}, copyErr
	}
	if n > maxBytes {
		_ = os.Remove(tmpPath)
		return PDFDocInfo{}, fmt.Errorf("PDF 文件超过最大限制 (%d MB)", maxBytes>>20)
	}
	return w.RegisterFile(tmpPath, name, "")
}

var pageSizeRe = regexp.MustCompile(`([0-9.]+)\s*x\s*([0-9.]+)\s*pts`)

func inspectPDFFile(pdfPath, password string) (PDFDocInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	args := []string{}
	if password != "" {
		args = append(args, "-upw", password)
	}
	args = append(args, pdfPath)

	cmd := exec.CommandContext(ctx, "pdfinfo", args...)
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil {
		if strings.Contains(strings.ToLower(text), "incorrect password") {
			return PDFDocInfo{Encrypted: true}, errors.New("PDF 已加密，请先使用「解密」功能输入密码解锁")
		}
		// Fallback to qpdf --show-npages if pdfinfo fails
		qArgs := []string{"--show-npages", pdfPath}
		if password != "" {
			qArgs = []string{"--password=" + password, "--show-npages", pdfPath}
		}
		qOut, qErr := exec.CommandContext(ctx, "qpdf", qArgs...).CombinedOutput()
		if qErr != nil {
			return PDFDocInfo{}, fmt.Errorf("无法解析 PDF 文件: %s", strings.TrimSpace(text))
		}
		nPages, _ := strconv.Atoi(strings.TrimSpace(string(qOut)))
		if nPages <= 0 {
			return PDFDocInfo{}, errors.New("PDF 页数无效")
		}
		return PDFDocInfo{
			Pages:        nPages,
			PageWidthPt:  595,
			PageHeightPt: 842,
		}, nil
	}

	info := PDFDocInfo{
		PageWidthPt:  595,
		PageHeightPt: 842,
	}
	for _, line := range strings.Split(text, "\n") {
		idx := strings.IndexByte(line, ':')
		if idx < 0 {
			continue
		}
		k := strings.TrimSpace(line[:idx])
		v := strings.TrimSpace(line[idx+1:])
		switch k {
		case "Title":
			info.Title = v
		case "Author":
			info.Author = v
		case "Pages":
			info.Pages, _ = strconv.Atoi(v)
		case "Encrypted":
			info.Encrypted = strings.HasPrefix(strings.ToLower(v), "yes")
		case "Page size":
			if m := pageSizeRe.FindStringSubmatch(v); len(m) == 3 {
				if wPt, err := strconv.ParseFloat(m[1], 64); err == nil && wPt > 0 {
					info.PageWidthPt = wPt
				}
				if hPt, err := strconv.ParseFloat(m[2], 64); err == nil && hPt > 0 {
					info.PageHeightPt = hPt
				}
			}
		}
	}
	if info.Pages <= 0 {
		return PDFDocInfo{}, errors.New("未能识别 PDF 页数")
	}
	return info, nil
}

// RenderPageJPEG 将指定 PDF 的第 page 页（1-based）渲染为 JPEG 字节。
func (w *PDFWorkspace) RenderPageJPEG(id string, page, dpi int) ([]byte, error) {
	pdfPath, info, err := w.GetPath(id)
	if err != nil {
		return nil, err
	}
	if page < 1 || page > info.Pages {
		return nil, fmt.Errorf("页码超出范围 (1~%d)", info.Pages)
	}
	if dpi < 36 {
		dpi = 36
	}
	if dpi > 220 {
		dpi = 220
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tmpPrefix := filepath.Join(w.rootDir, fmt.Sprintf("render-%s-%d-%d-%d", id, page, dpi, time.Now().UnixNano()))
	defer func() {
		_ = os.Remove(tmpPrefix + ".jpg")
	}()

	cmd := exec.CommandContext(ctx, "pdftoppm",
		"-jpeg",
		"-jpegopt", "quality=86",
		"-r", strconv.Itoa(dpi),
		"-f", strconv.Itoa(page),
		"-l", strconv.Itoa(page),
		"-singlefile",
		pdfPath,
		tmpPrefix,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("渲染页面失败: %s", strings.TrimSpace(string(out)))
	}
	return os.ReadFile(tmpPrefix + ".jpg")
}

// ExtractText 提取全文或单页文本（page == 0 表示提取全文）。
func (w *PDFWorkspace) ExtractText(id string, page int) (string, error) {
	pdfPath, info, err := w.GetPath(id)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	args := []string{"-enc", "UTF-8"}
	if page >= 1 && page <= info.Pages {
		args = append(args, "-f", strconv.Itoa(page), "-l", strconv.Itoa(page))
	}
	args = append(args, pdfPath, "-")
	out, err := exec.CommandContext(ctx, "pdftotext", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("提取文本失败: %s", strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// ArrangePages 按给定的页面序列（可跨多个文档、可重排/删除/提取/单页旋转）生成新 PDF。
func (w *PDFWorkspace) ArrangePages(specs []PDFPageSpec, outName string) (PDFDocInfo, error) {
	if len(specs) == 0 {
		return PDFDocInfo{}, errors.New("至少需要保留 1 页")
	}
	if len(specs) > 2000 {
		return PDFDocInfo{}, errors.New("单次重排页数超出上限")
	}

	// Verify all source docs & build qpdf arguments
	// First step: assemble pages in order via qpdf --empty --pages ...
	tmpAssembled, err := os.CreateTemp(w.rootDir, "arrange-step1-*.pdf")
	if err != nil {
		return PDFDocInfo{}, err
	}
	tmpAssembledPath := tmpAssembled.Name()
	_ = tmpAssembled.Close()

	args := []string{"--empty", "--pages"}
	var rotations []string
	for idx, sp := range specs {
		srcPath, srcInfo, err := w.GetPath(sp.DocID)
		if err != nil {
			_ = os.Remove(tmpAssembledPath)
			return PDFDocInfo{}, err
		}
		if sp.Page < 1 || sp.Page > srcInfo.Pages {
			_ = os.Remove(tmpAssembledPath)
			return PDFDocInfo{}, fmt.Errorf("源文档 %s 页码 %d 超出范围", srcInfo.Name, sp.Page)
		}
		args = append(args, srcPath, strconv.Itoa(sp.Page))
		rot := ((sp.Rotate % 360) + 360) % 360
		if rot == 90 || rot == 180 || rot == 270 {
			rotations = append(rotations, fmt.Sprintf("--rotate=+%d:%d", rot, idx+1))
		}
	}
	args = append(args, "--", tmpAssembledPath)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	if out, err := exec.CommandContext(ctx, "qpdf", args...).CombinedOutput(); err != nil {
		_ = os.Remove(tmpAssembledPath)
		return PDFDocInfo{}, fmt.Errorf("qpdf 组装页面失败: %s", strings.TrimSpace(string(out)))
	}

	finalTmpPath := tmpAssembledPath
	if len(rotations) > 0 {
		tmpRotated, err := os.CreateTemp(w.rootDir, "arrange-step2-*.pdf")
		if err != nil {
			_ = os.Remove(tmpAssembledPath)
			return PDFDocInfo{}, err
		}
		tmpRotatedPath := tmpRotated.Name()
		_ = tmpRotated.Close()

		rotArgs := []string{tmpAssembledPath}
		rotArgs = append(rotArgs, rotations...)
		rotArgs = append(rotArgs, tmpRotatedPath)
		if out, err := exec.CommandContext(ctx, "qpdf", rotArgs...).CombinedOutput(); err != nil {
			_ = os.Remove(tmpAssembledPath)
			_ = os.Remove(tmpRotatedPath)
			return PDFDocInfo{}, fmt.Errorf("qpdf 旋转页面失败: %s", strings.TrimSpace(string(out)))
		}
		_ = os.Remove(tmpAssembledPath)
		finalTmpPath = tmpRotatedPath
	}

	if outName == "" {
		outName = "arranged.pdf"
	}
	return w.RegisterFile(finalTmpPath, outName, "")
}

// ParsePageRangeExpr 解析形如 "1-3, 5, 8-10" 的页码表达式（1..maxPage）。
func ParsePageRangeExpr(expr string, maxPage int) ([]int, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, errors.New("页码范围不能为空")
	}
	var pages []int
	for _, part := range strings.Split(expr, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			startStr := strings.TrimSpace(bounds[0])
			endStr := strings.TrimSpace(bounds[1])
			if strings.EqualFold(endStr, "z") || strings.EqualFold(endStr, "end") {
				endStr = strconv.Itoa(maxPage)
			}
			s, err1 := strconv.Atoi(startStr)
			e, err2 := strconv.Atoi(endStr)
			if err1 != nil || err2 != nil || s < 1 || e < 1 || s > maxPage || e > maxPage {
				return nil, fmt.Errorf("无效的页码区间: %s (总页数 %d)", part, maxPage)
			}
			if s <= e {
				for p := s; p <= e; p++ {
					pages = append(pages, p)
				}
			} else {
				for p := s; p >= e; p-- {
					pages = append(pages, p)
				}
			}
		} else {
			p, err := strconv.Atoi(part)
			if err != nil || p < 1 || p > maxPage {
				return nil, fmt.Errorf("无效页码: %s (总页数 %d)", part, maxPage)
			}
			pages = append(pages, p)
		}
	}
	if len(pages) == 0 {
		return nil, errors.New("未解析出有效页码")
	}
	return pages, nil
}

// OptimizePDF 使用 Ghostscript 压缩 PDF 或将其转为灰度文档。
func (w *PDFWorkspace) OptimizePDF(id, quality string, grayscale bool) (PDFDocInfo, error) {
	srcPath, srcInfo, err := w.GetPath(id)
	if err != nil {
		return PDFDocInfo{}, err
	}
	tmpOut, err := os.CreateTemp(w.rootDir, "optimize-*.pdf")
	if err != nil {
		return PDFDocInfo{}, err
	}
	tmpOutPath := tmpOut.Name()
	_ = tmpOut.Close()

	pdfSetting := "/ebook"
	switch quality {
	case "screen":
		pdfSetting = "/screen"
	case "printer":
		pdfSetting = "/printer"
	case "default":
		pdfSetting = "/default"
	}

	args := []string{
		"-sDEVICE=pdfwrite",
		"-dCompatibilityLevel=1.4",
		"-dPDFSETTINGS=" + pdfSetting,
		"-dNOPAUSE",
		"-dQUIET",
		"-dBATCH",
	}
	if grayscale {
		args = append(args,
			"-sColorConversionStrategy=Gray",
			"-dProcessColorModel=/DeviceGray",
		)
	}
	args = append(args, "-sOutputFile="+tmpOutPath, srcPath)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if out, err := exec.CommandContext(ctx, "gs", args...).CombinedOutput(); err != nil {
		_ = os.Remove(tmpOutPath)
		return PDFDocInfo{}, fmt.Errorf("Ghostscript 优化失败: %s", strings.TrimSpace(string(out)))
	}

	suffix := "-compressed.pdf"
	if grayscale {
		suffix = "-grayscale.pdf"
	}
	base := strings.TrimSuffix(srcInfo.Name, filepath.Ext(srcInfo.Name))
	return w.RegisterFile(tmpOutPath, base+suffix, "")
}

// EncryptOrDecryptPDF 对 PDF 进行 AES-256 加密或输入密码解密。
func (w *PDFWorkspace) EncryptOrDecryptPDF(id, mode, password string) (PDFDocInfo, error) {
	srcPath, srcInfo, err := w.GetPath(id)
	if err != nil {
		return PDFDocInfo{}, err
	}
	password = strings.TrimSpace(password)
	if password == "" {
		return PDFDocInfo{}, errors.New("密码不能为空")
	}

	tmpOut, err := os.CreateTemp(w.rootDir, "security-*.pdf")
	if err != nil {
		return PDFDocInfo{}, err
	}
	tmpOutPath := tmpOut.Name()
	_ = tmpOut.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var args []string
	var checkPw string
	base := strings.TrimSuffix(srcInfo.Name, filepath.Ext(srcInfo.Name))
	var outName string

	if mode == "decrypt" {
		args = []string{"--password=" + password, "--decrypt", srcPath, tmpOutPath}
		outName = base + "-unlocked.pdf"
	} else {
		args = []string{"--encrypt", password, password, "256", "--", srcPath, tmpOutPath}
		checkPw = password
		outName = base + "-encrypted.pdf"
	}

	if out, err := exec.CommandContext(ctx, "qpdf", args...).CombinedOutput(); err != nil {
		_ = os.Remove(tmpOutPath)
		return PDFDocInfo{}, fmt.Errorf("qpdf 加解密失败: %s", strings.TrimSpace(string(out)))
	}
	return w.RegisterFile(tmpOutPath, outName, checkPw)
}

// ApplyStampOverlay 接收前端 Canvas 渲染的透明 PNG 水印（或使用纯 Go 生成的矢量文字水印）并通过 qpdf --overlay 叠加到所有页面。
func (w *PDFWorkspace) ApplyStampOverlay(id, watermarkText string, opacity float64, pngDataURL string) (PDFDocInfo, error) {
	srcPath, srcInfo, err := w.GetPath(id)
	if err != nil {
		return PDFDocInfo{}, err
	}
	if opacity <= 0 || opacity > 1 {
		opacity = 0.22
	}

	stampTmp, err := os.CreateTemp(w.rootDir, "stamp-*.pdf")
	if err != nil {
		return PDFDocInfo{}, err
	}
	stampPath := stampTmp.Name()
	_ = stampTmp.Close()
	defer os.Remove(stampPath)

	var stampBytes []byte
	if strings.HasPrefix(pngDataURL, "data:image/png;base64,") {
		rawPng, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(pngDataURL, "data:image/png;base64,"))
		if err != nil {
			return PDFDocInfo{}, errors.New("无效的水印图像数据")
		}
		stampBytes, err = buildPNGAlphaStampPDF(rawPng, srcInfo.PageWidthPt, srcInfo.PageHeightPt)
		if err != nil {
			return PDFDocInfo{}, err
		}
	} else {
		if strings.TrimSpace(watermarkText) == "" {
			return PDFDocInfo{}, errors.New("水印文字不能为空")
		}
		stampBytes = buildVectorWatermarkPDF(watermarkText, srcInfo.PageWidthPt, srcInfo.PageHeightPt, opacity)
	}

	if err := os.WriteFile(stampPath, stampBytes, 0o600); err != nil {
		return PDFDocInfo{}, err
	}

	outTmp, err := os.CreateTemp(w.rootDir, "watermarked-*.pdf")
	if err != nil {
		return PDFDocInfo{}, err
	}
	outTmpPath := outTmp.Name()
	_ = outTmp.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	args := []string{srcPath, "--overlay", stampPath, "--repeat=1-z", "--", outTmpPath}
	if out, err := exec.CommandContext(ctx, "qpdf", args...).CombinedOutput(); err != nil {
		_ = os.Remove(outTmpPath)
		return PDFDocInfo{}, fmt.Errorf("叠加水印失败: %s", strings.TrimSpace(string(out)))
	}

	base := strings.TrimSuffix(srcInfo.Name, filepath.Ext(srcInfo.Name))
	return w.RegisterFile(outTmpPath, base+"-watermarked.pdf", "")
}

// ExportImagesZip 将 PDF 全部页面渲染为 PNG 并打包成 ZIP 流输出。
func (w *PDFWorkspace) ExportImagesZip(id string, dpi int, zw io.Writer) error {
	pdfPath, info, err := w.GetPath(id)
	if err != nil {
		return err
	}
	if dpi < 72 {
		dpi = 72
	}
	if dpi > 200 {
		dpi = 200
	}

	tmpDir, err := os.MkdirTemp(w.rootDir, "export-imgs-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	prefix := filepath.Join(tmpDir, "page")
	cmd := exec.CommandContext(ctx, "pdftoppm", "-png", "-r", strconv.Itoa(dpi), pdfPath, prefix)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("导出图片失败: %s", strings.TrimSpace(string(out)))
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return err
	}

	archive := zip.NewWriter(zw)
	base := strings.TrimSuffix(info.Name, filepath.Ext(info.Name))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".png") {
			continue
		}
		f, err := os.Open(filepath.Join(tmpDir, e.Name()))
		if err != nil {
			continue
		}
		wEntry, err := archive.Create(base + "-" + e.Name())
		if err == nil {
			_, _ = io.Copy(wEntry, f)
		}
		_ = f.Close()
	}
	return archive.Close()
}

// ImagesToPDF 将一组上传的图片（JPEG/PNG）合成为一个多页 PDF。
func (w *PDFWorkspace) ImagesToPDF(images [][]byte, outName string) (PDFDocInfo, error) {
	if len(images) == 0 {
		return PDFDocInfo{}, errors.New("请至少提供 1 张图片")
	}
	pdfBytes, err := buildImagesPDF(images)
	if err != nil {
		return PDFDocInfo{}, err
	}
	tmp, err := os.CreateTemp(w.rootDir, "img2pdf-*.pdf")
	if err != nil {
		return PDFDocInfo{}, err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(pdfBytes); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return PDFDocInfo{}, err
	}
	_ = tmp.Close()
	if outName == "" {
		outName = "images-merged.pdf"
	}
	return w.RegisterFile(tmpPath, outName, "")
}

// CreateSamplePDF 生成一份 6 页带色块、页码、标题和说明的示例 PDF 文档供一键体验。
func (w *PDFWorkspace) CreateSamplePDF() (PDFDocInfo, error) {
	pdfBytes := buildSampleMultiPagePDF()
	tmp, err := os.CreateTemp(w.rootDir, "sample-*.pdf")
	if err != nil {
		return PDFDocInfo{}, err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(pdfBytes); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return PDFDocInfo{}, err
	}
	_ = tmp.Close()
	return w.RegisterFile(tmpPath, "OmniHub-LocalPDF-Sample-6Pages.pdf", "")
}

func pdfEscapeString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`, "\r", "", "\n", " ")
	return r.Replace(s)
}

func assemblePDFObjects(objects []string, rootObjNum int) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")
	offsets := make([]int, len(objects)+1)
	for i, obj := range objects {
		objNum := i + 1
		offsets[objNum] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", objNum, obj)
	}
	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, rootObjNum, xrefOffset)
	return buf.Bytes()
}

func buildVectorWatermarkPDF(text string, widthPt, heightPt, opacity float64) []byte {
	if widthPt <= 0 {
		widthPt = 595
	}
	if heightPt <= 0 {
		heightPt = 842
	}
	gray := 1.0 - opacity*0.75
	if gray < 0.25 {
		gray = 0.25
	}
	if gray > 0.92 {
		gray = 0.92
	}
	safeText := pdfEscapeString(text)
	var stream bytes.Buffer
	fmt.Fprintf(&stream, "q\n%.2f g\n", gray)
	// Tiled 3x3 diagonal watermark across the page
	for row := 0; row < 3; row++ {
		for col := 0; col < 2; col++ {
			x := widthPt*(0.18+0.45*float64(col))
			y := heightPt*(0.20+0.30*float64(row))
			// 35 degree rotation matrix: cos(35)=0.819, sin(35)=0.574
			fmt.Fprintf(&stream, "BT /F1 28 Tf 0.819 0.574 -0.574 0.819 %.1f %.1f Tm (%s) Tj ET\n", x, y, safeText)
		}
	}
	stream.WriteString("Q\n")
	sBytes := stream.String()

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>", widthPt, heightPt),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(sBytes), sBytes),
	}
	return assemblePDFObjects(objs, 1)
}

func buildPNGAlphaStampPDF(pngBytes []byte, widthPt, heightPt float64) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, errors.New("空水印图片")
	}

	rgbBuf := make([]byte, 0, w*h*3)
	alphaBuf := make([]byte, 0, w*h)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			nrgba := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			rgbBuf = append(rgbBuf, nrgba.R, nrgba.G, nrgba.B)
			alphaBuf = append(alphaBuf, nrgba.A)
		}
	}

	content := fmt.Sprintf("q %.2f 0 0 %.2f 0 0 cm /Im1 Do Q\n", widthPt, heightPt)
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /XObject << /Im1 4 0 R >> >> /Contents 6 0 R >>", widthPt, heightPt),
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /SMask 5 0 R /Length %d >>\nstream\n%s\nendstream", w, h, len(rgbBuf), string(rgbBuf)),
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8 /Length %d >>\nstream\n%s\nendstream", w, h, len(alphaBuf), string(alphaBuf)),
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content),
	}
	return assemblePDFObjects(objs, 1), nil
}

func buildImagesPDF(rawImages [][]byte) ([]byte, error) {
	type pageImg struct {
		jpegBytes []byte
		w, h      int
	}
	var pages []pageImg
	for _, raw := range rawImages {
		img, _, err := image.Decode(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("无法解码图片: %w", err)
		}
		b := img.Bounds()
		if b.Dx() <= 0 || b.Dy() <= 0 {
			continue
		}
		// Flatten onto white background & encode as JPEG for clean PDF embedding
		canvas := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
		draw.Draw(canvas, canvas.Bounds(), img, b.Min, draw.Over)
		var jBuf bytes.Buffer
		if err := jpeg.Encode(&jBuf, canvas, &jpeg.Options{Quality: 88}); err != nil {
			return nil, err
		}
		pages = append(pages, pageImg{jpegBytes: jBuf.Bytes(), w: b.Dx(), h: b.Dy()})
	}
	if len(pages) == 0 {
		return nil, errors.New("没有有效图片")
	}

	// Object 1: Catalog, Object 2: Pages
	objs := make([]string, 2)
	objs[0] = "<< /Type /Catalog /Pages 2 0 R >>"
	var kids []string

	for _, p := range pages {
		// Fit onto A4 width (595pt) preserving aspect ratio
		pageW := 595.0
		pageH := pageW * float64(p.h) / float64(p.w)
		if pageH < 200 {
			pageH = 200
		}
		pageObjNum := len(objs) + 1
		imgObjNum := len(objs) + 2
		cntObjNum := len(objs) + 3
		kids = append(kids, fmt.Sprintf("%d 0 R", pageObjNum))

		content := fmt.Sprintf("q %.2f 0 0 %.2f 0 0 cm /Im1 Do Q\n", pageW, pageH)
		pageObj := fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /XObject << /Im1 %d 0 R >> >> /Contents %d 0 R >>", pageW, pageH, imgObjNum, cntObjNum)
		imgObj := fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream", p.w, p.h, len(p.jpegBytes), string(p.jpegBytes))
		cntObj := fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content)
		objs = append(objs, pageObj, imgObj, cntObj)
	}
	objs[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pages))
	return assemblePDFObjects(objs, 1), nil
}

func buildSampleMultiPagePDF() []byte {
	const numPages = 6
	colors := [][3]float64{
		{0.15, 0.38, 0.72},
		{0.12, 0.58, 0.45},
		{0.68, 0.26, 0.42},
		{0.78, 0.44, 0.14},
		{0.38, 0.25, 0.68},
		{0.18, 0.48, 0.56},
	}
	titles := []string{
		"Page 1 - Cover & Overview",
		"Page 2 - Architecture & Modules",
		"Page 3 - Page Reorder & Rotation",
		"Page 4 - Split & Merge Pipeline",
		"Page 5 - Watermark & Grayscale",
		"Page 6 - Security & Text Extraction",
	}

	objs := make([]string, 3)
	objs[0] = "<< /Type /Catalog /Pages 2 0 R >>"
	objs[2] = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>"
	var kids []string

	for i := 0; i < numPages; i++ {
		pageObjNum := len(objs) + 1
		cntObjNum := len(objs) + 2
		kids = append(kids, fmt.Sprintf("%d 0 R", pageObjNum))
		c := colors[i%len(colors)]

		var s bytes.Buffer
		// Background card & header banner
		fmt.Fprintf(&s, "q\n0.96 0.97 0.99 rg 28 28 539 786 re f\n")
		fmt.Fprintf(&s, "%.2f %.2f %.2f rg 28 700 539 114 re f\n", c[0], c[1], c[2])
		// Decorative geometric badge for clear orientation inspection when rotated
		fmt.Fprintf(&s, "%.2f %.2f %.2f rg 60 420 180 180 re f\n", c[0]*0.9, c[1]*0.9, c[2]*0.9)
		fmt.Fprintf(&s, "1 1 1 rg\n")
		fmt.Fprintf(&s, "BT /F1 28 Tf 1 0 0 1 54 755 Tm (OmniHub LocalPDF Workbench) Tj ET\n")
		fmt.Fprintf(&s, "BT /F1 18 Tf 1 0 0 1 54 722 Tm (%s) Tj ET\n", pdfEscapeString(titles[i]))
		fmt.Fprintf(&s, "BT /F1 64 Tf 1 0 0 1 95 490 Tm (P%d) Tj ET\n", i+1)
		fmt.Fprintf(&s, "0.18 0.22 0.28 rg\n")
		fmt.Fprintf(&s, "BT /F1 16 Tf 1 0 0 1 270 560 Tm (Sample Document Page %d of %d) Tj ET\n", i+1, numPages)
		fmt.Fprintf(&s, "BT /F1 12 Tf 1 0 0 1 270 525 Tm (Use this page to test rotation, reordering,) Tj ET\n")
		fmt.Fprintf(&s, "BT /F1 12 Tf 1 0 0 1 270 502 Tm (page extraction, watermark overlay, grayscale,) Tj ET\n")
		fmt.Fprintf(&s, "BT /F1 12 Tf 1 0 0 1 270 479 Tm (encryption, and PNG/Text export.) Tj ET\n")
		fmt.Fprintf(&s, "0.45 0.50 0.58 rg\n")
		fmt.Fprintf(&s, "BT /F1 12 Tf 1 0 0 1 250 56 Tm (- Page %d / %d -) Tj ET\n", i+1, numPages)
		s.WriteString("Q\n")
		cntStr := s.String()

		pageObj := fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", cntObjNum)
		cntObj := fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(cntStr), cntStr)
		objs = append(objs, pageObj, cntObj)
	}
	objs[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), numPages)
	return assemblePDFObjects(objs, 1)
}
