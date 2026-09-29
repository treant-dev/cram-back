package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// seekingStore stands in for S3 as the SDK behaves over plain HTTP: a body it cannot seek is
// refused outright, which is how every upload to the in-network MinIO used to fail.
type seekingStore struct {
	got         []byte
	contentType string
}

func (s *seekingStore) Upload(_ context.Context, r io.Reader, _ int64, contentType, ext string) (string, error) {
	if _, ok := r.(io.Seeker); !ok {
		return "", errors.New("unseekable stream is not supported without TLS and trailing checksum")
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	s.got, s.contentType = b, contentType
	return "https://s3.example/cram/x" + ext, nil
}

func TestUploadSendsTheWholeFileAsASeekableBody(t *testing.T) {
	// A PNG signature and then some: the handler reads the first bytes to sniff the type, and
	// those bytes must still be part of what is stored.
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 2000)...)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "pic.png")
	fw.Write(png)
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	store := &seekingStore{}
	NewUploadHandler(store).Upload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(store.got, png) {
		t.Fatalf("stored %d bytes, want the %d uploaded", len(store.got), len(png))
	}
	if store.contentType != "image/png" {
		t.Errorf("content type = %q, want image/png", store.contentType)
	}
}
