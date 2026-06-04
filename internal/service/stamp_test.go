package service

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"drawingService/internal/model"
)

func testJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 240, G: uint8(x % 255), B: uint8(y % 255), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func TestDrawingServiceStampTextCRUD(t *testing.T) {
	svc, _ := newTestService(t, 0)

	stamp, err := svc.CreateStamp(context.Background(), StampInput{
		Input: model.DrawingStampInput{
			Name:      "  Евгений  ",
			TextValue: "  Evgeny  ",
			Priority:  model.StampPriorityText,
		},
		Actor: "user@example.com",
	})
	if err != nil {
		t.Fatalf("create stamp: %v", err)
	}
	if stamp.Name != "Евгений" || stamp.TextValue != "Evgeny" || stamp.HasImage {
		t.Fatalf("unexpected stamp: %#v", stamp)
	}

	items, err := svc.ListStamps(context.Background())
	if err != nil {
		t.Fatalf("list stamps: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one stamp, got %d", len(items))
	}

	if err := svc.DeleteStamp(context.Background(), stamp.ID); err != nil {
		t.Fatalf("delete stamp: %v", err)
	}
}

func TestDrawingServiceStampImageIsCompressedToPNG(t *testing.T) {
	svc, storage := newTestService(t, 0)
	svc.WithStampLimits(5*1024*1024, 64)
	source := testJPEG(t, 240, 120)

	stamp, err := svc.CreateStamp(context.Background(), StampInput{
		Input: model.DrawingStampInput{
			Name:      "Печать",
			TextValue: "Seal",
			Priority:  model.StampPriorityImage,
		},
		Filename: "seal.jpg",
		MimeType: "image/jpeg",
		Body:     bytes.NewReader(source),
		Actor:    "user@example.com",
	})
	if err != nil {
		t.Fatalf("create image stamp: %v", err)
	}
	if !stamp.HasImage || stamp.ImageMimeType != model.DefaultMimeType {
		t.Fatalf("expected png image stamp, got %#v", stamp)
	}
	if stamp.ImageWidth != 64 || stamp.ImageHeight != 32 {
		t.Fatalf("expected resized dimensions 64x32, got %dx%d", stamp.ImageWidth, stamp.ImageHeight)
	}

	body, mime, err := svc.DownloadStampImage(context.Background(), stamp.ID)
	if err != nil {
		t.Fatalf("download stamp: %v", err)
	}
	defer body.Close()
	if mime != model.DefaultMimeType {
		t.Fatalf("expected png mime, got %q", mime)
	}
	if _, err := png.Decode(body); err != nil {
		t.Fatalf("expected png content: %v", err)
	}
	if !storage.HasFile(stamp.ImageDriveFileID) {
		t.Fatalf("expected stamp image in storage")
	}
}

func TestDrawingServiceReplacesDuplicateStampName(t *testing.T) {
	svc, storage := newTestService(t, 0)
	svc.WithStampLimits(5*1024*1024, 64)
	source := testJPEG(t, 240, 120)

	first, err := svc.CreateStamp(context.Background(), StampInput{
		Input: model.DrawingStampInput{
			Name:      " Антон ",
			TextValue: "Anton",
			Priority:  model.StampPriorityImage,
		},
		Filename: "anton-first.jpg",
		MimeType: "image/jpeg",
		Body:     bytes.NewReader(source),
		Actor:    "user@example.com",
	})
	if err != nil {
		t.Fatalf("create first stamp: %v", err)
	}

	second, err := svc.CreateStamp(context.Background(), StampInput{
		Input: model.DrawingStampInput{
			Name:      "антон",
			TextValue: "Anton 2",
			Priority:  model.StampPriorityImage,
		},
		Filename: "anton-second.jpg",
		MimeType: "image/jpeg",
		Body:     bytes.NewReader(source),
		Actor:    "user@example.com",
	})
	if err != nil {
		t.Fatalf("create duplicate stamp: %v", err)
	}

	items, err := svc.ListStamps(context.Background())
	if err != nil {
		t.Fatalf("list stamps: %v", err)
	}
	if len(items) != 1 || items[0].ID != second.ID {
		t.Fatalf("expected duplicate name to be replaced, got %#v", items)
	}
	if storage.HasFile(first.ImageDriveFileID) {
		t.Fatalf("expected old duplicate image file to be deleted")
	}
	if !storage.HasFile(second.ImageDriveFileID) {
		t.Fatalf("expected new stamp image file to remain")
	}
}

func TestDrawingServiceListStampsCleansExistingDuplicateNames(t *testing.T) {
	svc, storage := newTestService(t, 0)
	firstFileID, err := storage.UploadPNG(context.Background(), "anton-old.png", bytes.NewReader(pngBytes(64)), 64)
	if err != nil {
		t.Fatalf("upload first: %v", err)
	}
	secondFileID, err := storage.UploadPNG(context.Background(), "anton-new.png", bytes.NewReader(pngBytes(64)), 64)
	if err != nil {
		t.Fatalf("upload second: %v", err)
	}
	first, err := svc.repo.CreateStamp(model.DrawingStampInput{
		Name:      "Антон",
		TextValue: "old",
		Priority:  model.StampPriorityImage,
	}, firstFileID, 64, model.DefaultMimeType, 10, 10, "user@example.com")
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := svc.repo.CreateStamp(model.DrawingStampInput{
		Name:      " антон ",
		TextValue: "new",
		Priority:  model.StampPriorityImage,
	}, secondFileID, 64, model.DefaultMimeType, 10, 10, "user@example.com")
	if err != nil {
		t.Fatalf("create second: %v", err)
	}

	items, err := svc.ListStamps(context.Background())
	if err != nil {
		t.Fatalf("list stamps: %v", err)
	}
	if len(items) != 1 || items[0].ID != second.ID {
		t.Fatalf("expected newest duplicate to remain, got %#v", items)
	}
	if storage.HasFile(first.ImageDriveFileID) {
		t.Fatalf("expected old duplicate image file to be deleted")
	}
	if !storage.HasFile(second.ImageDriveFileID) {
		t.Fatalf("expected newest duplicate image file to remain")
	}
}
