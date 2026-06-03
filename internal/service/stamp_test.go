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

	items, err := svc.ListStamps()
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
