package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"

	"drawingService/internal/google"
	"drawingService/internal/model"
	"drawingService/internal/store"
)

var (
	ErrUnsupportedMime = errors.New("only image/png is allowed")
	ErrEmptyPayload     = errors.New("file is empty")
	ErrPayloadTooLarge  = errors.New("file is too large")
)

type DrawingService struct {
	repo         *store.DrawingRepository
	storage      google.Storage
	maxImageBytes int64
}

func NewDrawingService(repo *store.DrawingRepository, storage google.Storage, maxImageBytes int64) *DrawingService {
	if maxImageBytes <= 0 {
		maxImageBytes = 10 * 1024 * 1024
	}
	return &DrawingService{repo: repo, storage: storage, maxImageBytes: maxImageBytes}
}

func (s *DrawingService) List() ([]model.DrawingImage, error) {
	return s.repo.List()
}

func (s *DrawingService) MaxFileBytes() int64 {
	return s.maxImageBytes
}

func (s *DrawingService) Get(id string) (model.DrawingImage, error) {
	if id == "" {
		return model.DrawingImage{}, store.ErrNotFound
	}
	return s.repo.Find(id)
}

func (s *DrawingService) Download(ctx context.Context, id string) (io.ReadCloser, string, error) {
	image, err := s.repo.FindWithDriveID(id)
	if err != nil {
		return nil, "", err
	}
	return s.storage.Download(ctx, image.DriveFileID)
}

type CreateInput struct {
	Input     model.DrawingImageInput
	Filename  string
	MimeType  string
	Body      io.Reader
	Size      int64
	Actor     string
}

func (s *DrawingService) Create(ctx context.Context, in CreateInput) (model.DrawingImage, error) {
	mime := strings.ToLower(strings.TrimSpace(in.MimeType))
	if mime == "" {
		mime = model.DefaultMimeType
	}
	if mime != model.DefaultMimeType {
		return model.DrawingImage{}, ErrUnsupportedMime
	}
	data, err := readAllLimited(in.Body, s.maxImageBytes)
	if err != nil {
		return model.DrawingImage{}, err
	}
	if len(data) == 0 {
		return model.DrawingImage{}, ErrEmptyPayload
	}

	driveName := buildDriveName(in.Filename, in.Input.Title)
	fileID, err := s.storage.UploadPNG(ctx, driveName, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return model.DrawingImage{}, fmt.Errorf("upload png: %w", err)
	}

	image, err := s.repo.Create(in.Input, fileID, int64(len(data)), mime, in.Actor)
	if err != nil {
		if cleanupErr := s.storage.Delete(ctx, fileID); cleanupErr != nil {
			log.Printf("drawing service: failed to cleanup drive file %q after repo create error: %v", fileID, cleanupErr)
		}
		return model.DrawingImage{}, err
	}
	// Return the image with DriveFileID set so callers can use it directly.
	image.DriveFileID = fileID
	return image, nil
}

type UpdateInput struct {
	Input    model.DrawingImageInput
	Filename string
	MimeType string
	Body     io.Reader
	Size     int64
	Actor    string
}

func (s *DrawingService) Update(ctx context.Context, id string, in UpdateInput) (model.DrawingImage, error) {
	existing, err := s.repo.FindWithDriveID(id)
	if err != nil {
		return model.DrawingImage{}, err
	}

	mime := strings.ToLower(strings.TrimSpace(in.MimeType))
	if mime == "" {
		mime = model.DefaultMimeType
	}
	if mime != model.DefaultMimeType {
		return model.DrawingImage{}, ErrUnsupportedMime
	}
	data, err := readAllLimited(in.Body, s.maxImageBytes)
	if err != nil {
		return model.DrawingImage{}, err
	}
	if len(data) == 0 {
		return model.DrawingImage{}, ErrEmptyPayload
	}

	if err := s.storage.UpdatePNG(ctx, existing.DriveFileID, bytes.NewReader(data), int64(len(data))); err != nil {
		return model.DrawingImage{}, fmt.Errorf("update png: %w", err)
	}

	return s.repo.Update(id, in.Input, int64(len(data)), mime, in.Actor)
}

func (s *DrawingService) Delete(ctx context.Context, id string) error {
	existing, err := s.repo.FindWithDriveID(id)
	if err != nil {
		return err
	}
	if err := s.storage.Delete(ctx, existing.DriveFileID); err != nil {
		return fmt.Errorf("storage delete: %w", err)
	}
	if _, err := s.repo.Delete(id); err != nil {
		return err
	}
	return nil
}

func readAllLimited(r io.Reader, limit int64) ([]byte, error) {
	if r == nil {
		return nil, ErrEmptyPayload
	}
	buf := &bytes.Buffer{}
	limited := io.LimitReader(r, limit+1)
	n, err := io.Copy(buf, limited)
	if err != nil {
		return nil, err
	}
	if n > limit {
		return nil, ErrPayloadTooLarge
	}
	return buf.Bytes(), nil
}

func buildDriveName(original, title string) string {
	ext := filepath.Ext(original)
	if ext == "" {
		ext = ".png"
	}
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', '\n', '\r', '\t':
			return '_'
		}
		if r < 32 {
			return -1
		}
		return r
	}, title)
	if cleaned == "" {
		cleaned = "drawing"
	}
	return cleaned + ext
}
