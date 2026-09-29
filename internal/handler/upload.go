package handler

import (
	"context"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
)

const (
	maxUploadSize = 5 << 20 // 5 MB
)

var allowedTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

type uploader interface {
	Upload(ctx context.Context, r io.Reader, size int64, contentType, ext string) (string, error)
}

type UploadHandler struct {
	store uploader
}

func NewUploadHandler(store uploader) *UploadHandler {
	if store == nil {
		return &UploadHandler{}
	}
	return &UploadHandler{store: store}
}

// POST /upload
func (h *UploadHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		http.Error(w, "file uploads not configured", http.StatusServiceUnavailable)
		return
	}
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		http.Error(w, "file too large (max 5 MB)", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Detect content type from the first 512 bytes.
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	contentType := http.DetectContentType(buf[:n])
	// Normalise: DetectContentType may return "image/jpeg" or variants.
	contentType = strings.Split(contentType, ";")[0]

	ext, ok := allowedTypes[contentType]
	if !ok {
		// Fall back to extension from filename.
		ext = strings.ToLower(filepath.Ext(header.Filename))
		switch ext {
		case ".jpg", ".jpeg":
			contentType = "image/jpeg"
		case ".png":
			contentType = "image/png"
		case ".webp":
			contentType = "image/webp"
		case ".gif":
			contentType = "image/gif"
		default:
			http.Error(w, "unsupported file type", http.StatusBadRequest)
			return
		}
	}

	// Rewind rather than stitching the sniffed bytes back on with a MultiReader: the S3 SDK
	// checksums the body before sending it, and over plain HTTP it can only do that with a
	// stream it can seek. A MultiReader failed every upload against the in-network MinIO.
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		log.Printf("upload: rewind: %v", err)
		http.Error(w, "upload failed", http.StatusInternalServerError)
		return
	}
	url, err := h.store.Upload(r.Context(), file, header.Size, contentType, ext)
	if err != nil {
		log.Printf("upload: %v", err)
		http.Error(w, "upload failed", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}
