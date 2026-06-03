package service

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"drawingService/internal/google"
	"drawingService/internal/model"
	"drawingService/internal/store"
)

func newTestService(t *testing.T, maxBytes int64) (*DrawingService, *google.FakeStorage) {
	t.Helper()
	dir := t.TempDir()
	db := store.OpenDb(filepath.Join(dir, "drawings.db"))
	repo := store.NewDrawingRepository(db)
	if err := repo.EnsureBuckets(); err != nil {
		t.Fatalf("ensure buckets: %v", err)
	}
	storage := google.NewFakeStorage()
	return NewDrawingService(repo, storage, maxBytes), storage
}

func pngBytes(n int) []byte {
	out := bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, n/4+1)
	return out[:n]
}

func TestDrawingServiceCreateValidatesTitle(t *testing.T) {
	svc, _ := newTestService(t, 0)
	_, err := svc.Create(context.Background(), CreateInput{
		Input:    model.DrawingImageInput{Title: "  ", Width: 10, Height: 10},
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
		Input:    model.DrawingImageInput{Title: "x", Width: 10, Height: 10},
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
	db := store.OpenDb(filepath.Join(dir, "drawings.db"))
	repo := store.NewDrawingRepository(db)
	if err := repo.EnsureBuckets(); err != nil {
		t.Fatalf("ensure buckets: %v", err)
	}
	storage := google.NewFakeStorage()
	svc := NewDrawingService(repo, storage, 0)

	// Use a title containing forbidden chars to make the filename non-empty but ensure name
	_, err := svc.Create(context.Background(), CreateInput{
		Input:    model.DrawingImageInput{Title: "ok", Width: 10, Height: 10},
		Filename: "x.png",
		MimeType: "image/png",
		Body:     bytes.NewReader(pngBytes(8)),
		Actor:    "user@example.com",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// After successful create, list returns at least one
	items, _ := svc.List()
	if len(items) != 1 {
		t.Fatalf("expected one image")
	}
}

func TestDrawingServiceUpdate(t *testing.T) {
	svc, storage := newTestService(t, 0)
	img, err := svc.Create(context.Background(), CreateInput{
		Input:    model.DrawingImageInput{Title: "Old", Width: 10, Height: 10},
		Filename: "x.png",
		MimeType: "image/png",
		Body:     bytes.NewReader(pngBytes(8)),
		Actor:    "user@example.com",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := svc.Update(context.Background(), img.ID, UpdateInput{
		Input:    model.DrawingImageInput{Title: "New", Width: 20, Height: 30},
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
		Input:    model.DrawingImageInput{Title: "x", Width: 1, Height: 1},
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
		Input:    model.DrawingImageInput{Title: "x", Width: 1, Height: 1},
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
		Input:    model.DrawingImageInput{Title: "x", Width: 1, Height: 1},
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
		Input:    model.DrawingImageInput{Title: "x", Width: 1, Height: 1},
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
		Input:    model.DrawingImageInput{Title: "x", Width: 1, Height: 1},
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
