package store

import (
	"errors"
	"testing"

	"drawingService/internal/model"
)

func TestDrawingRepositoryStampCRUD(t *testing.T) {
	repo := newTestRepo(t)

	stamp, err := repo.CreateStamp(model.DrawingStampInput{
		Name:      "  Евгений  ",
		TextValue: "  Evgeny  ",
		Priority:  model.StampPriorityText,
	}, "", 0, "", 0, 0, "user@example.com")
	if err != nil {
		t.Fatalf("create stamp: %v", err)
	}
	if stamp.Name != "Евгений" || stamp.TextValue != "Evgeny" {
		t.Fatalf("expected trimmed stamp, got %#v", stamp)
	}
	if stamp.HasImage {
		t.Fatalf("text stamp must not have image")
	}

	items, err := repo.ListStamps()
	if err != nil {
		t.Fatalf("list stamps: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one stamp, got %d", len(items))
	}

	updated, err := repo.UpdateStamp(stamp.ID, model.DrawingStampInput{
		Name:      "Печать",
		TextValue: "Seal",
		Priority:  model.StampPriorityImage,
	}, "drive-stamp", 123, model.DefaultMimeType, 512, 256, false, "other@example.com")
	if err != nil {
		t.Fatalf("update stamp: %v", err)
	}
	if !updated.HasImage || updated.ImageSize != 123 || updated.Priority != model.StampPriorityImage {
		t.Fatalf("expected image stamp, got %#v", updated)
	}

	withDrive, err := repo.FindStampWithDriveID(stamp.ID)
	if err != nil {
		t.Fatalf("find with drive: %v", err)
	}
	if withDrive.ImageDriveFileID != "drive-stamp" {
		t.Fatalf("expected drive id, got %q", withDrive.ImageDriveFileID)
	}

	removed, err := repo.DeleteStamp(stamp.ID)
	if err != nil {
		t.Fatalf("delete stamp: %v", err)
	}
	if removed.ImageDriveFileID != "drive-stamp" {
		t.Fatalf("expected removed drive id, got %q", removed.ImageDriveFileID)
	}
	if _, err := repo.FindStamp(stamp.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDrawingRepositoryDeletesDuplicateStampsByName(t *testing.T) {
	repo := newTestRepo(t)

	first, err := repo.CreateStamp(model.DrawingStampInput{
		Name:      "  Антон  ",
		TextValue: "A",
		Priority:  model.StampPriorityImage,
	}, "drive-first", 10, model.DefaultMimeType, 20, 20, "user@example.com")
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := repo.CreateStamp(model.DrawingStampInput{
		Name:      "антон",
		TextValue: "B",
		Priority:  model.StampPriorityText,
	}, "", 0, "", 0, 0, "user@example.com")
	if err != nil {
		t.Fatalf("create second: %v", err)
	}

	removed, err := repo.DeleteDuplicateStampsByName(" Антон ", second.ID)
	if err != nil {
		t.Fatalf("delete duplicates: %v", err)
	}
	if len(removed) != 1 || removed[0].ID != first.ID || removed[0].ImageDriveFileID != "drive-first" {
		t.Fatalf("unexpected removed duplicates: %#v", removed)
	}
	if _, err := repo.FindStamp(first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected first stamp removed, got %v", err)
	}
	items, err := repo.ListStamps()
	if err != nil {
		t.Fatalf("list stamps: %v", err)
	}
	if len(items) != 1 || items[0].ID != second.ID {
		t.Fatalf("expected only kept stamp, got %#v", items)
	}
}
