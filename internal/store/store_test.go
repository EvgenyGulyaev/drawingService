package store

import (
	"path/filepath"
	"testing"
)

func TestOpenDbReturnsErrorForInvalidPath(t *testing.T) {
	dir := t.TempDir()
	_, err := OpenDb(filepath.Join(dir, "missing", "drawing.db"))
	if err == nil {
		t.Fatal("expected error for invalid db path")
	}
}
