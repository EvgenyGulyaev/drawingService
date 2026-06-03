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
