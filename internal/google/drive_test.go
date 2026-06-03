package google

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestFakeStorageLifecycle(t *testing.T) {
	ctx := context.Background()
	storage := NewFakeStorage()

	id, err := storage.UploadPNG(ctx, "a.png", bytes.NewReader([]byte("hello")), 5)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if id == "" {
		t.Fatalf("expected id")
	}

	reader, mime, err := storage.Download(ctx, id)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer reader.Close()
	if mime != "image/png" {
		t.Fatalf("expected image/png, got %q", mime)
	}
	buf := make([]byte, 5)
	if _, err := reader.Read(buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "hello" {
		t.Fatalf("expected hello, got %q", string(buf))
	}

	if err := storage.UpdatePNG(ctx, id, bytes.NewReader([]byte("world!")), 6); err != nil {
		t.Fatalf("update: %v", err)
	}
	reader, _, err = storage.Download(ctx, id)
	if err != nil {
		t.Fatalf("download after update: %v", err)
	}
	defer reader.Close()
	buf = make([]byte, 6)
	reader.Read(buf)
	if string(buf) != "world!" {
		t.Fatalf("expected world!, got %q", string(buf))
	}

	if err := storage.Delete(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, _, err := storage.Download(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFakeStorageDeleteMissingIsNoop(t *testing.T) {
	storage := NewFakeStorage()
	if err := storage.Delete(context.Background(), "missing"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestFakeStorageUploadRejectsInvalidArgs(t *testing.T) {
	storage := NewFakeStorage()
	ctx := context.Background()

	if _, err := storage.UploadPNG(ctx, "", bytes.NewReader([]byte("x")), 1); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("expected ErrInvalidName, got %v", err)
	}
	if _, err := storage.UploadPNG(ctx, "x", nil, 1); err == nil {
		t.Fatalf("expected error for nil body")
	}
	if _, err := storage.UploadPNG(ctx, "x", bytes.NewReader([]byte("x")), 0); err == nil {
		t.Fatalf("expected error for zero size")
	}
}
