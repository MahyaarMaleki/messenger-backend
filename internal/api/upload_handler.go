package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

const (
	MaxUploadSize = 10 << 20 // 10 MB
	UploadDir     = "./uploads"
)

type uploadResponse struct {
	URL  string `json:"url"`
	Name string `json:"name"`
	Type string `json:"type"`
}

func (server *Server) uploadFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadSize)
	if err := r.ParseMultipartForm(MaxUploadSize); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse("File too big"))
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse("Invalid file"))
		return
	}
	defer file.Close()

	// Create unique name
	ext := filepath.Ext(header.Filename)
	uniqueName := fmt.Sprintf("%s%s", uuid.New().String(), ext)

	os.MkdirAll(UploadDir, os.ModePerm)
	dstPath := filepath.Join(UploadDir, uniqueName)

	dst, err := os.Create(dstPath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errorResponse("Failed to save file"))
		return
	}
	defer dst.Close()

	io.Copy(dst, file)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(uploadResponse{
		URL:  fmt.Sprintf("/static/%s", uniqueName),
		Name: header.Filename,
		Type: header.Header.Get("Content-Type"),
	})
}
