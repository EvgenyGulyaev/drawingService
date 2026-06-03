package model

import (
	"errors"
	"time"
)

const (
	TitleMaxLength  = 120
	DefaultMimeType = "image/png"
	DrawingIDFormat = "%020d"
)

var (
	ErrTitleRequired = errors.New("title is required")
	ErrTitleTooLong  = errors.New("title is too long")
)

type DrawingImage struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	DriveFileID string    `json:"-"`
	MimeType    string    `json:"mime_type"`
	Size        int64     `json:"size"`
	Width       int       `json:"width"`
	Height      int       `json:"height"`
	CreatedBy   string    `json:"created_by"`
	UpdatedBy   string    `json:"updated_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type DrawingImageInput struct {
	Title  string `json:"title"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func NormalizeTitle(value string) (string, error) {
	trimmed := trimSpaces(value)
	if trimmed == "" {
		return "", ErrTitleRequired
	}
	if len([]rune(trimmed)) > TitleMaxLength {
		return "", ErrTitleTooLong
	}
	return trimmed, nil
}

func trimSpaces(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
