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
	"time"

	"drawingService/internal/google"
	"drawingService/internal/model"
	"drawingService/internal/store"
)

var (
	ErrUnsupportedMime = errors.New("only image/png is allowed")
	ErrEmptyPayload    = errors.New("file is empty")
	ErrPayloadTooLarge = errors.New("file is too large")
)

type DrawingService struct {
	repo          *store.DrawingRepository
	storage       google.Storage
	maxImageBytes int64
	driveTimeout  time.Duration
}

func NewDrawingService(repo *store.DrawingRepository, storage google.Storage, maxImageBytes int64) *DrawingService {
	if maxImageBytes <= 0 {
		maxImageBytes = 10 * 1024 * 1024
	}
	return &DrawingService{
		repo:          repo,
		storage:       storage,
		maxImageBytes: maxImageBytes,
		driveTimeout:  60 * time.Second,
	}
}

func (s *DrawingService) List(ctx context.Context) ([]model.DrawingImage, error) {
	items, err := s.repo.List()
	if err != nil {
		return nil, err
	}
	result := make([]model.DrawingImage, 0, len(items))
	for _, item := range items {
		stored, err := s.repo.FindWithDriveID(item.ID)
		if err != nil {
			return nil, err
		}
		exists, err := s.storage.Exists(ctx, stored.DriveFileID)
		if err != nil {
			return nil, err
		}
		if !exists {
			if _, err := s.repo.Delete(item.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
				return nil, err
			}
			continue
		}
		result = append(result, item)
	}
	return result, nil
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
	Input    model.DrawingImageInput
	Filename string
	MimeType string
	Body     io.Reader
	Size     int64
	Actor    string
}

func (s *DrawingService) Create(ctx context.Context, in CreateInput) (model.DrawingImage, error) {
	mime := strings.ToLower(strings.TrimSpace(in.MimeType))
	if mime == "" {
		mime = model.DefaultMimeType
	}
	if mime != model.DefaultMimeType {
		return model.DrawingImage{}, ErrUnsupportedMime
	}
	if err := in.Input.ValidateDimensions(); err != nil {
		return model.DrawingImage{}, err
	}
	data, err := readAllLimited(in.Body, s.maxImageBytes)
	if err != nil {
		return model.DrawingImage{}, err
	}
	if len(data) == 0 {
		return model.DrawingImage{}, ErrEmptyPayload
	}

	driveName := buildDriveName(in.Filename, in.Input.Title)
	driveCtx, driveCancel := s.driveContext(ctx)
	defer driveCancel()
	fileID, err := s.storage.UploadPNG(driveCtx, driveName, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return model.DrawingImage{}, fmt.Errorf("upload png: %w", err)
	}

	image, err := s.repo.Create(in.Input, fileID, int64(len(data)), mime, in.Actor)
	if err != nil {
		if cleanupErr := s.storage.Delete(driveCtx, fileID); cleanupErr != nil {
			log.Printf("drawing service: failed to cleanup drive file %q after repo create error: %v", fileID, cleanupErr)
		}
		return model.DrawingImage{}, err
	}
	// DriveFileID is only available on this in-memory object for immediate use
	// (e.g. in tests). It is never serialized to JSON (model.DriveFileID has json:"-").
	// External callers must use FindWithDriveID to get the persistent DriveFileID.
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
	if err := in.Input.ValidateDimensions(); err != nil {
		return model.DrawingImage{}, err
	}
	data, err := readAllLimited(in.Body, s.maxImageBytes)
	if err != nil {
		return model.DrawingImage{}, err
	}
	if len(data) == 0 {
		return model.DrawingImage{}, ErrEmptyPayload
	}

	driveCtx, driveCancel := s.driveContext(ctx)
	defer driveCancel()
	if err := s.storage.UpdatePNG(driveCtx, existing.DriveFileID, bytes.NewReader(data), int64(len(data))); err != nil {
		return model.DrawingImage{}, fmt.Errorf("update png: %w", err)
	}

	return s.repo.Update(id, in.Input, int64(len(data)), mime, in.Actor)
}

func (s *DrawingService) Delete(ctx context.Context, id string) error {
	existing, err := s.repo.FindWithDriveID(id)
	if err != nil {
		return err
	}
	driveCtx, driveCancel := s.driveContext(ctx)
	defer driveCancel()
	if err := s.storage.Delete(driveCtx, existing.DriveFileID); err != nil {
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

func (s *DrawingService) driveContext(parent context.Context) (context.Context, context.CancelFunc) {
	if s.driveTimeout <= 0 {
		return parent, func() {}
	}
	return context.WithTimeout(parent, s.driveTimeout)
}
