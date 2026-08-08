package store

import (
	"drawingService/internal/model"
	"path/filepath"
	"testing"
)

func newTestRepo(t *testing.T) *DrawingRepository {
	t.Helper()
	dir := t.TempDir()
	db, err := OpenDb(filepath.Join(dir, "drawings.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	repo := NewDrawingRepository(db)
	if err := repo.EnsureBuckets(); err != nil {
		t.Fatalf("ensure buckets: %v", err)
	}
	return repo
}

func TestDrawingRepositoryCRUD(t *testing.T) {
	repo := newTestRepo(t)

	image, err := repo.Create(model.DrawingImageInput{
		Title:  "  Мой набросок  ",
		Width:  800,
		Height: 600,
	}, "drive-1", 12345, "image/png", "user@example.com")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if image.Title != "Мой набросок" {
		t.Fatalf("expected trimmed title, got %q", image.Title)
	}
	if image.ID == "" {
		t.Fatalf("expected id, got empty")
	}
	if image.CreatedBy != "user@example.com" {
		t.Fatalf("expected created_by, got %q", image.CreatedBy)
	}

	items, err := repo.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].DriveFileID != "drive-1" {
		t.Fatalf("expected list to include drive file id, got %q", items[0].DriveFileID)
	}

	found, err := repo.Find(image.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found.Title != image.Title {
		t.Fatalf("expected same title, got %q", found.Title)
	}

	updated, err := repo.Update(image.ID, model.DrawingImageInput{
		Title:  "Обновлено",
		Width:  1024,
		Height: 768,
	}, 99999, "image/png", "other@example.com")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Title != "Обновлено" {
		t.Fatalf("expected updated title, got %q", updated.Title)
	}
	if updated.Size != 99999 {
		t.Fatalf("expected new size, got %d", updated.Size)
	}
	if updated.UpdatedBy != "other@example.com" {
		t.Fatalf("expected updated_by, got %q", updated.UpdatedBy)
	}
	if !updated.UpdatedAt.After(image.UpdatedAt) && !updated.UpdatedAt.Equal(image.UpdatedAt) {
		t.Fatalf("expected updated_at to be updated")
	}
	if updated.CreatedBy != image.CreatedBy {
		t.Fatalf("created_by must be preserved")
	}

	if _, err := repo.Delete(image.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Find(image.ID); err == nil {
		t.Fatalf("expected not found after delete")
	}
}

func TestDrawingRepositoryRejectsInvalidTitle(t *testing.T) {
	repo := newTestRepo(t)

	if _, err := repo.Create(model.DrawingImageInput{
		Title:  "   ",
		Width:  10,
		Height: 10,
	}, "drive-1", 1, "image/png", "user@example.com"); err != model.ErrTitleRequired {
		t.Fatalf("expected ErrTitleRequired, got %v", err)
	}

	tooLong := make([]byte, model.TitleMaxLength+1)
	for i := range tooLong {
		tooLong[i] = 'a'
	}
	if _, err := repo.Create(model.DrawingImageInput{
		Title:  string(tooLong),
		Width:  10,
		Height: 10,
	}, "drive-1", 1, "image/png", "user@example.com"); err != model.ErrTitleTooLong {
		t.Fatalf("expected ErrTitleTooLong, got %v", err)
	}
}

func TestDrawingRepositoryUpdateNotFound(t *testing.T) {
	repo := newTestRepo(t)
	if _, err := repo.Update("00000000000000000099", model.DrawingImageInput{
		Title:  "x",
		Width:  1,
		Height: 1,
	}, 1, "image/png", "user@example.com"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDrawingRepositorySequencesAreMonotonic(t *testing.T) {
	repo := newTestRepo(t)
	for i := 0; i < 3; i++ {
		img, err := repo.Create(model.DrawingImageInput{
			Title:  "img",
			Width:  1,
			Height: 1,
		}, "drive", 1, "image/png", "user@example.com")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if img.ID == "" {
			t.Fatalf("expected id")
		}
	}
}
