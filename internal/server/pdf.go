package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"omnihub/internal/tools"
)

const maxPDFUploadBytes = 128 << 20

func (r *Router) handleToolsPDFDocs(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "pdf", false) {
		return
	}
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var sharedPDFs []string
	if dir, err := r.sharedDir(); err == nil {
		if list, err := tools.ListSharedFiles(dir); err == nil {
			for _, f := range list {
				if !f.IsDir && strings.HasSuffix(strings.ToLower(f.Name), ".pdf") {
					sharedPDFs = append(sharedPDFs, f.Name)
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"docs":        r.pdfWork.List(),
		"shared_pdfs": sharedPDFs,
	})
}

func (r *Router) handleToolsPDFUpload(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "pdf", true) {
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := req.URL.Query().Get("filename")
	if name == "" {
		name = "uploaded.pdf"
	}
	body := http.MaxBytesReader(w, req.Body, maxPDFUploadBytes+1024)
	defer body.Close()

	info, err := r.pdfWork.ImportReader(name, body, maxPDFUploadBytes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (r *Router) handleToolsPDFImportShared(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "pdf", false) {
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var p struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 4096)).Decode(&p); err != nil || strings.TrimSpace(p.Name) == "" {
		http.Error(w, "invalid file name", http.StatusBadRequest)
		return
	}
	dir, err := r.sharedDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	f, _, err := tools.OpenShared(dir, p.Name)
	if err != nil {
		http.Error(w, "shared file not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	info, err := r.pdfWork.ImportReader(p.Name, f, maxPDFUploadBytes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (r *Router) handleToolsPDFSample(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "pdf", false) {
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	info, err := r.pdfWork.CreateSamplePDF()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (r *Router) handleToolsPDFDelete(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "pdf", false) {
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var p struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 4096)).Decode(&p); err != nil || p.ID == "" {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	r.pdfWork.Delete(p.ID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (r *Router) handleToolsPDFRender(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "pdf", false) {
		return
	}
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := req.URL.Query().Get("id")
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	dpi, _ := strconv.Atoi(req.URL.Query().Get("dpi"))
	if page <= 0 {
		page = 1
	}
	if dpi <= 0 {
		dpi = 96
	}
	jpg, err := r.pdfWork.RenderPageJPEG(id, page, dpi)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(jpg)
}

func (r *Router) handleToolsPDFDownload(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "pdf", false) {
		return
	}
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := req.URL.Query().Get("id")
	pdfPath, info, err := r.pdfWork.GetPath(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	f, err := os.Open(pdfPath)
	if err != nil {
		http.Error(w, "file unavailable", http.StatusNotFound)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/pdf")
	if req.URL.Query().Get("inline") == "1" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", info.Name))
	} else {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", info.Name))
	}
	http.ServeContent(w, req, info.Name, time.Now(), f)
}

func (r *Router) handleToolsPDFSaveShared(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "pdf", true) {
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var p struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 4096)).Decode(&p); err != nil || p.ID == "" {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	pdfPath, info, err := r.pdfWork.GetPath(p.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	dir, err := r.sharedDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	saveName := strings.TrimSpace(p.Name)
	if saveName == "" {
		saveName = info.Name
	}
	if !strings.HasSuffix(strings.ToLower(saveName), ".pdf") {
		saveName += ".pdf"
	}
	f, err := os.Open(pdfPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	// Avoid collision if file already exists in sharedDir
	ext := filepath.Ext(saveName)
	base := strings.TrimSuffix(saveName, ext)
	candidate := saveName
	for attempt := 0; attempt < 20; attempt++ {
		if attempt > 0 {
			candidate = fmt.Sprintf("%s-%d%s", base, attempt, ext)
		}
		_, _ = f.Seek(0, io.SeekStart)
		err = tools.WriteChunk(dir, candidate, 0, true, f)
		if err == nil {
			writeJSON(w, http.StatusOK, map[string]string{"saved_name": candidate})
			return
		}
	}
	http.Error(w, "保存至共享文件失败: "+err.Error(), http.StatusBadRequest)
}

func (r *Router) handleToolsPDFArrange(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "pdf", false) {
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var p struct {
		Specs   []tools.PDFPageSpec `json:"specs"`
		OutName string              `json:"out_name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 512<<10)).Decode(&p); err != nil {
		http.Error(w, "invalid payload: "+err.Error(), http.StatusBadRequest)
		return
	}
	info, err := r.pdfWork.ArrangePages(p.Specs, p.OutName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (r *Router) handleToolsPDFSplit(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "pdf", false) {
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var p struct {
		ID     string `json:"id"`
		Mode   string `json:"mode"` // "range" or "every_n"
		Range  string `json:"range"`
		EveryN int    `json:"every_n"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 16<<10)).Decode(&p); err != nil || p.ID == "" {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	_, srcInfo, err := r.pdfWork.GetPath(p.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	base := strings.TrimSuffix(srcInfo.Name, filepath.Ext(srcInfo.Name))

	if p.Mode == "every_n" {
		step := p.EveryN
		if step <= 0 {
			step = 1
		}
		var created []tools.PDFDocInfo
		partIdx := 1
		for start := 1; start <= srcInfo.Pages; start += step {
			end := start + step - 1
			if end > srcInfo.Pages {
				end = srcInfo.Pages
			}
			var specs []tools.PDFPageSpec
			for pg := start; pg <= end; pg++ {
				specs = append(specs, tools.PDFPageSpec{DocID: p.ID, Page: pg, Rotate: 0})
			}
			outName := fmt.Sprintf("%s-part%d-p%d-%d.pdf", base, partIdx, start, end)
			doc, err := r.pdfWork.ArrangePages(specs, outName)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			created = append(created, doc)
			partIdx++
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"docs": created})
		return
	}

	pages, err := tools.ParsePageRangeExpr(p.Range, srcInfo.Pages)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var specs []tools.PDFPageSpec
	for _, pg := range pages {
		specs = append(specs, tools.PDFPageSpec{DocID: p.ID, Page: pg, Rotate: 0})
	}
	doc, err := r.pdfWork.ArrangePages(specs, fmt.Sprintf("%s-extracted.pdf", base))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"docs": []tools.PDFDocInfo{doc}})
}

func (r *Router) handleToolsPDFWatermark(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "pdf", false) {
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var p struct {
		ID         string  `json:"id"`
		Text       string  `json:"text"`
		Opacity    float64 `json:"opacity"`
		PNGDataURL string  `json:"png_data_url"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 8<<20)).Decode(&p); err != nil || p.ID == "" {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	info, err := r.pdfWork.ApplyStampOverlay(p.ID, p.Text, p.Opacity, p.PNGDataURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (r *Router) handleToolsPDFOptimize(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "pdf", false) {
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var p struct {
		ID        string `json:"id"`
		Quality   string `json:"quality"`
		Grayscale bool   `json:"grayscale"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 16<<10)).Decode(&p); err != nil || p.ID == "" {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	info, err := r.pdfWork.OptimizePDF(p.ID, p.Quality, p.Grayscale)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (r *Router) handleToolsPDFSecurity(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "pdf", false) {
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var p struct {
		ID       string `json:"id"`
		Mode     string `json:"mode"` // "encrypt" or "decrypt"
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 16<<10)).Decode(&p); err != nil || p.ID == "" {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	info, err := r.pdfWork.EncryptOrDecryptPDF(p.ID, p.Mode, p.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (r *Router) handleToolsPDFText(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "pdf", false) {
		return
	}
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := req.URL.Query().Get("id")
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	txt, err := r.pdfWork.ExtractText(id, page)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": txt})
}

func (r *Router) handleToolsPDFExportImages(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "pdf", false) {
		return
	}
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := req.URL.Query().Get("id")
	dpi, _ := strconv.Atoi(req.URL.Query().Get("dpi"))
	_, info, err := r.pdfWork.GetPath(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	var buf bytes.Buffer
	if err := r.pdfWork.ExportImagesZip(id, dpi, &buf); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	zipName := strings.TrimSuffix(info.Name, filepath.Ext(info.Name)) + "-images.zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", zipName))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func (r *Router) handleToolsPDFImagesToPDF(w http.ResponseWriter, req *http.Request) {
	if !r.toolsAllowed(w, req, "pdf", true) {
		return
	}
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var p struct {
		Images  []string `json:"images"` // base64 or data URLs
		OutName string   `json:"out_name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 64<<20)).Decode(&p); err != nil || len(p.Images) == 0 {
		http.Error(w, "invalid images payload", http.StatusBadRequest)
		return
	}
	var rawImgs [][]byte
	for _, s := range p.Images {
		if idx := strings.Index(s, ","); idx >= 0 {
			s = s[idx+1:]
		}
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			http.Error(w, "invalid base64 image", http.StatusBadRequest)
			return
		}
		rawImgs = append(rawImgs, b)
	}
	info, err := r.pdfWork.ImagesToPDF(rawImgs, p.OutName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, info)
}
