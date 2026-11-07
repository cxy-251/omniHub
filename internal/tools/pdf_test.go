package tools

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestPDFWorkspacePipeline(t *testing.T) {
	ws := NewPDFWorkspace(t.TempDir())

	// 1. Sample 6-page PDF creation & metadata inspection
	doc, err := ws.CreateSamplePDF()
	if err != nil {
		t.Fatalf("CreateSamplePDF failed: %v", err)
	}
	if doc.Pages != 6 {
		t.Fatalf("Expected 6 pages, got %d", doc.Pages)
	}

	// 2. Render page 1 JPEG thumbnail
	jpgBytes, err := ws.RenderPageJPEG(doc.ID, 1, 72)
	if err != nil {
		t.Fatalf("RenderPageJPEG failed: %v", err)
	}
	if len(jpgBytes) < 500 {
		t.Fatalf("Rendered JPEG too small: %d bytes", len(jpgBytes))
	}

	// 3. Extract text
	txt, err := ws.ExtractText(doc.ID, 1)
	if err != nil {
		t.Fatalf("ExtractText failed: %v", err)
	}
	if !strings.Contains(txt, "OmniHub LocalPDF") {
		t.Errorf("Extracted text missing expected title, got: %q", txt)
	}

	// 4. Arrange pages (reorder + rotate + extract subset)
	arranged, err := ws.ArrangePages([]PDFPageSpec{
		{DocID: doc.ID, Page: 3, Rotate: 90},
		{DocID: doc.ID, Page: 1, Rotate: 0},
		{DocID: doc.ID, Page: 5, Rotate: 180},
	}, "reordered.pdf")
	if err != nil {
		t.Fatalf("ArrangePages failed: %v", err)
	}
	if arranged.Pages != 3 {
		t.Fatalf("Expected 3 pages after arrange, got %d", arranged.Pages)
	}

	// 5. Watermark overlay (vector)
	wmDoc, err := ws.ApplyStampOverlay(arranged.ID, "CONFIDENTIAL", 0.3, "")
	if err != nil {
		t.Fatalf("ApplyStampOverlay failed: %v", err)
	}
	if wmDoc.Pages != 3 {
		t.Fatalf("Expected 3 pages after watermark, got %d", wmDoc.Pages)
	}

	// 6. Optimize & Grayscale via Ghostscript
	optDoc, err := ws.OptimizePDF(wmDoc.ID, "ebook", true)
	if err != nil {
		t.Fatalf("OptimizePDF failed: %v", err)
	}
	if optDoc.Pages != 3 {
		t.Fatalf("Expected 3 pages after optimize, got %d", optDoc.Pages)
	}

	// 7. Encrypt & Decrypt via qpdf
	encDoc, err := ws.EncryptOrDecryptPDF(optDoc.ID, "encrypt", "secret123")
	if err != nil {
		t.Fatalf("Encrypt PDF failed: %v", err)
	}
	if !encDoc.Encrypted {
		t.Errorf("Expected Encrypted=true on encrypted PDF")
	}
	decDoc, err := ws.EncryptOrDecryptPDF(encDoc.ID, "decrypt", "secret123")
	if err != nil {
		t.Fatalf("Decrypt PDF failed: %v", err)
	}
	if decDoc.Encrypted {
		t.Errorf("Expected Encrypted=false on decrypted PDF")
	}

	// 8. Images to PDF
	img := image.NewRGBA(image.Rect(0, 0, 200, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.RGBA{R: 40, G: 120, B: 200, A: 255})
		}
	}
	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, img)
	imgDoc, err := ws.ImagesToPDF([][]byte{pngBuf.Bytes(), pngBuf.Bytes()}, "from-images.pdf")
	if err != nil {
		t.Fatalf("ImagesToPDF failed: %v", err)
	}
	if imgDoc.Pages != 2 {
		t.Fatalf("Expected 2 pages from 2 images, got %d", imgDoc.Pages)
	}
}
