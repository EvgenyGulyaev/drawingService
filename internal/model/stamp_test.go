package model

import (
	"errors"
	"testing"
)

func TestNormalizeStampInputRequiresNameAndContent(t *testing.T) {
	_, err := NormalizeStampInput(DrawingStampInput{Name: "  ", TextValue: "x", Priority: StampPriorityText}, false)
	if !errors.Is(err, ErrStampNameRequired) {
		t.Fatalf("expected ErrStampNameRequired, got %v", err)
	}

	_, err = NormalizeStampInput(DrawingStampInput{Name: "x", Priority: StampPriorityText}, false)
	if !errors.Is(err, ErrStampContentRequired) {
		t.Fatalf("expected ErrStampContentRequired, got %v", err)
	}
}

func TestNormalizeStampInputValidatesPriorityContent(t *testing.T) {
	_, err := NormalizeStampInput(DrawingStampInput{Name: "x", TextValue: "Name", Priority: StampPriorityImage}, false)
	if !errors.Is(err, ErrStampPriorityInvalid) {
		t.Fatalf("expected ErrStampPriorityInvalid, got %v", err)
	}

	got, err := NormalizeStampInput(DrawingStampInput{Name: "  Stamp  ", TextValue: "  Name  ", Priority: StampPriorityText}, false)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got.Name != "Stamp" || got.TextValue != "Name" || got.Priority != StampPriorityText {
		t.Fatalf("unexpected normalized input: %#v", got)
	}
}
