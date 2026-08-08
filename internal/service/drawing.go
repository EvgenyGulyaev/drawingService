package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"log"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"drawingService/internal/google"
	"drawingService/internal/model"
	"drawingService/internal/store"

	_ "image/jpeg"
)

var (
	ErrUnsupportedMime = errors.New("only image/png is allowed")
	ErrEmptyPayload    = errors.New("file is empty")
	ErrPayloadTooLarge = errors.New("file is too large")
)

var ErrUnsupportedStampMime = errors.New("only image/png and image/jpeg are allowed for stamp images")

const driveExistsConcurrency = 4

type DrawingService struct {
	repo          *store.DrawingRepository
	storage       google.Storage
	stampStorage  google.Storage
	maxImageBytes int64
	maxStampBytes int64
	maxStampDim   int
	driveTimeout  time.Duration
}

func NewDrawingService(repo *store.DrawingRepository, storage google.Storage, maxImageBytes int64) *DrawingService {
	if maxImageBytes <= 0 {
		maxImageBytes = 10 * 1024 * 1024
	}
	return &DrawingService{
		repo:          repo,
		storage:       storage,
		stampStorage:  storage,
		maxImageBytes: maxImageBytes,
		maxStampBytes: 5 * 1024 * 1024,
		maxStampDim:   512,
		driveTimeout:  60 * time.Second,
	}
}

func (s *DrawingService) WithStampStorage(storage google.Storage) *DrawingService {
	if storage != nil {
		s.stampStorage = storage
	}
	return s
}

func (s *DrawingService) WithStampLimits(maxBytes int64, maxDim int) *DrawingService {
	if maxBytes > 0 {
		s.maxStampBytes = maxBytes
	}
	if maxDim > 0 {
		s.maxStampDim = maxDim
	}
	return s
}

func (s *DrawingService) List(ctx context.Context) ([]model.DrawingImage, error) {
	items, err := s.repo.List()
	if err != nil {
		return nil, err
	}
	checkCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	exists := make([]bool, len(items))
	sem := make(chan struct{}, driveExistsConcurrency)
	var wg sync.WaitGroup
	var errOnce sync.Once
	var firstErr error
	for i := range items {
		wg.Go(func() {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-checkCtx.Done():
				return
			}
			var checkErr error
			exists[i], checkErr = s.storage.Exists(checkCtx, items[i].DriveFileID)
			if checkErr != nil {
				errOnce.Do(func() {
					firstErr = checkErr
					cancel()
				})
			}
		})
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make([]model.DrawingImage, 0, len(items))
	for i, item := range items {
		if !exists[i] {
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

func (s *DrawingService) MaxStampBytes() int64 {
	return s.maxStampBytes
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

type StampInput struct {
	Input       model.DrawingStampInput
	Filename    string
	MimeType    string
	Body        io.Reader
	RemoveImage bool
	Actor       string
}

func (s *DrawingService) ListStamps(ctx context.Context) ([]model.DrawingStamp, error) {
	items, err := s.repo.ListStamps()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]string)
	cleaned := false
	for _, item := range items {
		key := strings.ToLower(strings.TrimSpace(item.Name))
		if key == "" {
			continue
		}
		keepID, ok := seen[key]
		if ok {
			s.cleanupDuplicateStampImages(ctx, item.Name, keepID)
			cleaned = true
			continue
		}
		seen[key] = item.ID
	}
	if !cleaned {
		return items, nil
	}
	return s.repo.ListStamps()
}

func (s *DrawingService) GetStamp(id string) (model.DrawingStamp, error) {
	return s.repo.FindStamp(id)
}

func (s *DrawingService) CreateStamp(ctx context.Context, in StampInput) (model.DrawingStamp, error) {
	var fileID string
	var size int64
	var width int
	var height int
	if in.Body != nil {
		processed, err := s.processStampImage(in.Body, in.MimeType)
		if err != nil {
			return model.DrawingStamp{}, err
		}
		driveName := buildDriveName(in.Filename, in.Input.Name)
		driveCtx, driveCancel := s.driveContext(ctx)
		defer driveCancel()
		fileID, err = s.stampStorage.UploadPNG(driveCtx, driveName, bytes.NewReader(processed.data), int64(len(processed.data)))
		if err != nil {
			return model.DrawingStamp{}, fmt.Errorf("upload stamp png: %w", err)
		}
		size = int64(len(processed.data))
		width = processed.width
		height = processed.height
	}
	stamp, err := s.repo.CreateStamp(in.Input, fileID, size, model.DefaultMimeType, width, height, in.Actor)
	if err != nil {
		if fileID != "" {
			driveCtx, driveCancel := s.driveContext(ctx)
			defer driveCancel()
			if cleanupErr := s.stampStorage.Delete(driveCtx, fileID); cleanupErr != nil {
				log.Printf("drawing service: failed to cleanup stamp drive file %q after repo create error: %v", fileID, cleanupErr)
			}
		}
		return model.DrawingStamp{}, err
	}
	s.cleanupDuplicateStampImages(ctx, stamp.Name, stamp.ID)
	return stamp, nil
}

func (s *DrawingService) UpdateStamp(ctx context.Context, id string, in StampInput) (model.DrawingStamp, error) {
	existing, err := s.repo.FindStampWithDriveID(id)
	if err != nil {
		return model.DrawingStamp{}, err
	}
	var fileID string
	var size int64
	var width int
	var height int
	if in.Body != nil {
		processed, err := s.processStampImage(in.Body, in.MimeType)
		if err != nil {
			return model.DrawingStamp{}, err
		}
		driveName := buildDriveName(in.Filename, in.Input.Name)
		driveCtx, driveCancel := s.driveContext(ctx)
		defer driveCancel()
		fileID, err = s.stampStorage.UploadPNG(driveCtx, driveName, bytes.NewReader(processed.data), int64(len(processed.data)))
		if err != nil {
			return model.DrawingStamp{}, fmt.Errorf("upload stamp png: %w", err)
		}
		size = int64(len(processed.data))
		width = processed.width
		height = processed.height
	}
	updated, err := s.repo.UpdateStamp(id, in.Input, fileID, size, model.DefaultMimeType, width, height, in.RemoveImage, in.Actor)
	if err != nil {
		if fileID != "" {
			driveCtx, driveCancel := s.driveContext(ctx)
			defer driveCancel()
			if cleanupErr := s.stampStorage.Delete(driveCtx, fileID); cleanupErr != nil {
				log.Printf("drawing service: failed to cleanup stamp drive file %q after repo update error: %v", fileID, cleanupErr)
			}
		}
		return model.DrawingStamp{}, err
	}
	if fileID != "" && existing.ImageDriveFileID != "" {
		driveCtx, driveCancel := s.driveContext(ctx)
		defer driveCancel()
		if err := s.stampStorage.Delete(driveCtx, existing.ImageDriveFileID); err != nil {
			log.Printf("drawing service: failed to delete old stamp drive file %q: %v", existing.ImageDriveFileID, err)
		}
	}
	if in.RemoveImage && existing.ImageDriveFileID != "" {
		driveCtx, driveCancel := s.driveContext(ctx)
		defer driveCancel()
		if err := s.stampStorage.Delete(driveCtx, existing.ImageDriveFileID); err != nil {
			log.Printf("drawing service: failed to delete removed stamp drive file %q: %v", existing.ImageDriveFileID, err)
		}
	}
	s.cleanupDuplicateStampImages(ctx, updated.Name, updated.ID)
	return updated, nil
}

func (s *DrawingService) cleanupDuplicateStampImages(ctx context.Context, name string, keepID string) {
	removed, err := s.repo.DeleteDuplicateStampsByName(name, keepID)
	if err != nil {
		log.Printf("drawing service: failed to cleanup duplicate stamps named %q: %v", name, err)
		return
	}
	if len(removed) == 0 {
		return
	}
	driveCtx, driveCancel := s.driveContext(ctx)
	defer driveCancel()
	for _, stamp := range removed {
		if stamp.ImageDriveFileID == "" {
			continue
		}
		if err := s.stampStorage.Delete(driveCtx, stamp.ImageDriveFileID); err != nil {
			log.Printf("drawing service: failed to delete duplicate stamp drive file %q: %v", stamp.ImageDriveFileID, err)
		}
	}
}

func (s *DrawingService) DownloadStampImage(ctx context.Context, id string) (io.ReadCloser, string, error) {
	stamp, err := s.repo.FindStampWithDriveID(id)
	if err != nil {
		return nil, "", err
	}
	if !stamp.HasImage || stamp.ImageDriveFileID == "" {
		return nil, "", store.ErrNotFound
	}
	return s.stampStorage.Download(ctx, stamp.ImageDriveFileID)
}

func (s *DrawingService) DeleteStamp(ctx context.Context, id string) error {
	existing, err := s.repo.FindStampWithDriveID(id)
	if err != nil {
		return err
	}
	if existing.ImageDriveFileID != "" {
		driveCtx, driveCancel := s.driveContext(ctx)
		defer driveCancel()
		if err := s.stampStorage.Delete(driveCtx, existing.ImageDriveFileID); err != nil {
			return fmt.Errorf("stamp storage delete: %w", err)
		}
	}
	_, err = s.repo.DeleteStamp(id)
	return err
}

type processedStampImage struct {
	data   []byte
	width  int
	height int
}

func (s *DrawingService) processStampImage(body io.Reader, mimeType string) (processedStampImage, error) {
	mime := strings.ToLower(strings.TrimSpace(mimeType))
	if mime == "" || mime == "application/octet-stream" {
		mime = model.DefaultMimeType
	}
	if mime != model.DefaultMimeType && mime != "image/jpeg" && mime != "image/jpg" {
		return processedStampImage{}, ErrUnsupportedStampMime
	}
	data, err := readAllLimited(body, s.maxStampBytes)
	if err != nil {
		return processedStampImage{}, err
	}
	if len(data) == 0 {
		return processedStampImage{}, ErrEmptyPayload
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return processedStampImage{}, fmt.Errorf("decode stamp image: %w", err)
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return processedStampImage{}, ErrEmptyPayload
	}
	targetW, targetH := fitDimensions(w, h, s.maxStampDim)
	dst := resizeNearest(src, targetW, targetH)
	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return processedStampImage{}, err
	}
	return processedStampImage{data: out.Bytes(), width: targetW, height: targetH}, nil
}

func resizeNearest(src image.Image, targetW, targetH int) *image.RGBA {
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	for y := 0; y < targetH; y++ {
		srcY := srcBounds.Min.Y + (y * srcH / targetH)
		for x := 0; x < targetW; x++ {
			srcX := srcBounds.Min.X + (x * srcW / targetW)
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	return dst
}

func fitDimensions(width, height, maxDim int) (int, int) {
	if maxDim <= 0 {
		maxDim = 512
	}
	if width <= maxDim && height <= maxDim {
		return width, height
	}
	scale := float64(maxDim) / float64(max(width, height))
	w := int(math.Round(float64(width) * scale))
	h := int(math.Round(float64(height) * scale))
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
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
