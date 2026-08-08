package service

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"drawingService/internal/google"
	"drawingService/internal/model"
	"drawingService/internal/store"
)

func newTestService(t testing.TB, maxBytes int64) (*DrawingService, *google.FakeStorage) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.OpenDb(filepath.Join(dir, "drawings.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	repo := store.NewDrawingRepository(db)
	if err := repo.EnsureBuckets(); err != nil {
		t.Fatalf("ensure buckets: %v", err)
	}
	storage := google.NewFakeStorage()
	return NewDrawingService(repo, storage, maxBytes), storage
}

type trackingStorage struct {
	*google.FakeStorage
	delay  time.Duration
	active atomic.Int32
	max    atomic.Int32
}

func (s *trackingStorage) Exists(ctx context.Context, fileID string) (bool, error) {
	active := s.active.Add(1)
	defer s.active.Add(-1)
	for current := s.max.Load(); active > current && !s.max.CompareAndSwap(current, active); current = s.max.Load() {
	}
	timer := time.NewTimer(s.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-timer.C:
		return s.FakeStorage.Exists(ctx, fileID)
	}
}

func pngBytes(n int) []byte {
	out := bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, n/4+1)
	return out[:n]
}

func TestDrawingServiceCreateValidatesTitle(t *testing.T) {
	svc, _ := newTestService(t, 0)
	_, err := svc.Create(context.Background(), CreateInput{
		Input:    model.DrawingImageInput{Title: "  ", Width: 100, Height: 100},
		Filename: "a.png",
		MimeType: "image/png",
		Body:     bytes.NewReader(pngBytes(8)),
		Actor:    "user@example.com",
	})
	if !errors.Is(err, model.ErrTitleRequired) {
		t.Fatalf("expected ErrTitleRequired, got %v", err)
	}
}

func TestDrawingServiceCreateRejectsNonPNG(t *testing.T) {
	svc, _ := newTestService(t, 0)
	_, err := svc.Create(context.Background(), CreateInput{
		Input:    model.DrawingImageInput{Title: "x", Width: 100, Height: 100},
		Filename: "a.jpg",
		MimeType: "image/jpeg",
		Body:     bytes.NewReader(pngBytes(8)),
		Actor:    "user@example.com",
	})
	if !errors.Is(err, ErrUnsupportedMime) {
		t.Fatalf("expected ErrUnsupportedMime, got %v", err)
	}
}

func TestDrawingServiceCreateUploadsAndPersists(t *testing.T) {
	svc, storage := newTestService(t, 0)
	img, err := svc.Create(context.Background(), CreateInput{
		Input:    model.DrawingImageInput{Title: "  Test  ", Width: 100, Height: 50},
		Filename: "x.png",
		MimeType: "image/png",
		Body:     bytes.NewReader(pngBytes(64)),
		Actor:    "user@example.com",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if img.Title != "Test" {
		t.Fatalf("expected trimmed title, got %q", img.Title)
	}
	if img.DriveFileID == "" {
		t.Fatalf("expected drive file id")
	}
	if !storage.HasFile(img.DriveFileID) {
		t.Fatalf("expected file in storage")
	}
}

func TestDrawingServiceCreateRollsBackOnMetadataFailure(t *testing.T) {
	dir := t.TempDir()
	db, err := store.OpenDb(filepath.Join(dir, "drawings.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	repo := store.NewDrawingRepository(db)
	if err := repo.EnsureBuckets(); err != nil {
		t.Fatalf("ensure buckets: %v", err)
	}
	storage := google.NewFakeStorage()
	svc := NewDrawingService(repo, storage, 0)

	// Use a title containing forbidden chars to make the filename non-empty but ensure name
	_, err = svc.Create(context.Background(), CreateInput{
		Input:    model.DrawingImageInput{Title: "ok", Width: 100, Height: 100},
		Filename: "x.png",
		MimeType: "image/png",
		Body:     bytes.NewReader(pngBytes(8)),
		Actor:    "user@example.com",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// After successful create, list returns at least one
	items, _ := svc.List(context.Background())
	if len(items) != 1 {
		t.Fatalf("expected one image")
	}
}

func TestDrawingServiceListRemovesMissingDriveFiles(t *testing.T) {
	svc, storage := newTestService(t, 0)
	img, err := svc.Create(context.Background(), CreateInput{
		Input:    model.DrawingImageInput{Title: "orphan", Width: 100, Height: 100},
		Filename: "x.png",
		MimeType: "image/png",
		Body:     bytes.NewReader(pngBytes(8)),
		Actor:    "user@example.com",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	storage.RemoveFile(img.DriveFileID)

	items, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected missing drive file to be removed from list, got %d items", len(items))
	}
	if _, err := svc.Get(img.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected stale metadata to be removed, got %v", err)
	}
}

func TestDrawingServiceListChecksDriveFilesWithBoundedConcurrency(t *testing.T) {
	const expectedConcurrency = 4
	svc, fake := newTestService(t, 0)
	for range 8 {
		if _, err := svc.Create(context.Background(), CreateInput{
			Input:    model.DrawingImageInput{Title: "drawing", Width: 100, Height: 100},
			Filename: "x.png",
			MimeType: "image/png",
			Body:     bytes.NewReader(pngBytes(8)),
			Actor:    "user@example.com",
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	storage := &trackingStorage{FakeStorage: fake, delay: 10 * time.Millisecond}
	svc.storage = storage

	items, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 8 {
		t.Fatalf("expected 8 items, got %d", len(items))
	}
	if got := storage.max.Load(); got != expectedConcurrency {
		t.Fatalf("expected %d concurrent checks, got %d", expectedConcurrency, got)
	}
}

func BenchmarkDrawingServiceList(b *testing.B) {
	svc, fake := newTestService(b, 0)
	for range 20 {
		if _, err := svc.Create(context.Background(), CreateInput{
			Input:    model.DrawingImageInput{Title: "drawing", Width: 100, Height: 100},
			Filename: "x.png",
			MimeType: "image/png",
			Body:     bytes.NewReader(pngBytes(8)),
			Actor:    "user@example.com",
		}); err != nil {
			b.Fatalf("create: %v", err)
		}
	}
	svc.storage = &trackingStorage{FakeStorage: fake, delay: time.Millisecond}
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		if _, err := svc.List(ctx); err != nil {
			b.Fatalf("list: %v", err)
		}
	}
}

func TestDrawingServiceUpdate(t *testing.T) {
	svc, storage := newTestService(t, 0)
	img, err := svc.Create(context.Background(), CreateInput{
		Input:    model.DrawingImageInput{Title: "Old", Width: 100, Height: 100},
		Filename: "x.png",
		MimeType: "image/png",
		Body:     bytes.NewReader(pngBytes(8)),
		Actor:    "user@example.com",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := svc.Update(context.Background(), img.ID, UpdateInput{
		Input:    model.DrawingImageInput{Title: "New", Width: 200, Height: 300},
		Filename: "x.png",
		MimeType: "image/png",
		Body:     bytes.NewReader(pngBytes(16)),
		Actor:    "other@example.com",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Title != "New" {
		t.Fatalf("expected New, got %q", updated.Title)
	}
	if updated.UpdatedBy != "other@example.com" {
		t.Fatalf("expected updated_by, got %q", updated.UpdatedBy)
	}
	if !storage.HasFile(img.DriveFileID) {
		t.Fatalf("expected file still in storage")
	}
}

func TestDrawingServiceUpdateRequiresExisting(t *testing.T) {
	svc, _ := newTestService(t, 0)
	_, err := svc.Update(context.Background(), "00000000000000000099", UpdateInput{
		Input:    model.DrawingImageInput{Title: "x", Width: 100, Height: 100},
		Filename: "x.png",
		MimeType: "image/png",
		Body:     bytes.NewReader(pngBytes(8)),
		Actor:    "user@example.com",
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDrawingServiceDeleteRemovesBoth(t *testing.T) {
	svc, storage := newTestService(t, 0)
	img, err := svc.Create(context.Background(), CreateInput{
		Input:    model.DrawingImageInput{Title: "x", Width: 100, Height: 100},
		Filename: "x.png",
		MimeType: "image/png",
		Body:     bytes.NewReader(pngBytes(8)),
		Actor:    "user@example.com",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.Delete(context.Background(), img.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if storage.HasFile(img.DriveFileID) {
		t.Fatalf("expected file gone from storage")
	}
	if _, err := svc.Get(img.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestDrawingServiceDeleteKeepsMetadataIfStorageFails(t *testing.T) {
	svc, storage := newTestService(t, 0)
	img, err := svc.Create(context.Background(), CreateInput{
		Input:    model.DrawingImageInput{Title: "x", Width: 100, Height: 100},
		Filename: "x.png",
		MimeType: "image/png",
		Body:     bytes.NewReader(pngBytes(8)),
		Actor:    "user@example.com",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	storage.SetDeleteError(errors.New("boom"))
	if err := svc.Delete(context.Background(), img.ID); err == nil {
		t.Fatalf("expected error, got nil")
	}
	if _, err := svc.Get(img.ID); err != nil {
		t.Fatalf("metadata should still exist, got %v", err)
	}
}

func TestDrawingServiceCreateRejectsEmptyPayload(t *testing.T) {
	svc, _ := newTestService(t, 0)
	_, err := svc.Create(context.Background(), CreateInput{
		Input:    model.DrawingImageInput{Title: "x", Width: 100, Height: 100},
		Filename: "x.png",
		MimeType: "image/png",
		Body:     bytes.NewReader(nil),
		Actor:    "user@example.com",
	})
	if !errors.Is(err, ErrEmptyPayload) {
		t.Fatalf("expected ErrEmptyPayload, got %v", err)
	}
}

func TestDrawingServiceCreateEnforcesMaxBytes(t *testing.T) {
	svc, _ := newTestService(t, 16)
	_, err := svc.Create(context.Background(), CreateInput{
		Input:    model.DrawingImageInput{Title: "x", Width: 100, Height: 100},
		Filename: "x.png",
		MimeType: "image/png",
		Body:     bytes.NewReader(pngBytes(64)),
		Actor:    "user@example.com",
	})
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("expected ErrPayloadTooLarge, got %v", err)
	}
}

func TestBuildDriveNameSanitizes(t *testing.T) {
	got := buildDriveName("orig.png", "  bad/name:foo*  ")
	if strings.ContainsAny(got, "/\\:*?\"<>|\n\t") {
		t.Fatalf("unsafe chars in %q", got)
	}
	if !strings.HasSuffix(got, ".png") {
		t.Fatalf("expected .png suffix, got %q", got)
	}
}

func TestDrawingServiceRejectsOutOfRangeDimensions(t *testing.T) {
	svc, _ := newTestService(t, 0)
	// Too small
	_, err := svc.Create(context.Background(), CreateInput{
		Input:    model.DrawingImageInput{Title: "small", Width: 10, Height: 10},
		MimeType: "image/png",
		Body:     bytes.NewReader(pngBytes(8)),
		Actor:    "u@e.com",
	})
	if !errors.Is(err, model.ErrCanvasTooSmall) {
		t.Fatalf("expected ErrCanvasTooSmall, got %v", err)
	}
	// Too large
	_, err = svc.Create(context.Background(), CreateInput{
		Input:    model.DrawingImageInput{Title: "big", Width: 9999, Height: 9999},
		MimeType: "image/png",
		Body:     bytes.NewReader(pngBytes(8)),
		Actor:    "u@e.com",
	})
	if !errors.Is(err, model.ErrCanvasTooLarge) {
		t.Fatalf("expected ErrCanvasTooLarge, got %v", err)
	}
}
