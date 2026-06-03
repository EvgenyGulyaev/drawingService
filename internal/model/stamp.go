package model

import (
	"errors"
	"time"
)

const (
	StampPriorityText  = "text"
	StampPriorityImage = "image"
	StampIDFormat      = "%020d"
	StampNameMaxLength = 120
	StampTextMaxLength = 240
)

var (
	ErrStampNameRequired    = errors.New("stamp name is required")
	ErrStampNameTooLong     = errors.New("stamp name is too long")
	ErrStampTextTooLong     = errors.New("stamp text is too long")
	ErrStampContentRequired = errors.New("stamp text or image is required")
	ErrStampPriorityInvalid = errors.New("stamp priority does not match available content")
)

type DrawingStamp struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	TextValue        string    `json:"textValue"`
	ImageDriveFileID string    `json:"-"`
	HasImage         bool      `json:"hasImage"`
	ImageMimeType    string    `json:"imageMimeType"`
	ImageSize        int64     `json:"imageSize"`
	ImageWidth       int       `json:"imageWidth"`
	ImageHeight      int       `json:"imageHeight"`
	Priority         string    `json:"priority"`
	CreatedBy        string    `json:"createdBy"`
	UpdatedBy        string    `json:"updatedBy"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type DrawingStampInput struct {
	Name      string `json:"name"`
	TextValue string `json:"textValue"`
	Priority  string `json:"priority"`
}

func NormalizeStampInput(input DrawingStampInput, hasImage bool) (DrawingStampInput, error) {
	name := trimSpaces(input.Name)
	if name == "" {
		return DrawingStampInput{}, ErrStampNameRequired
	}
	if len([]rune(name)) > StampNameMaxLength {
		return DrawingStampInput{}, ErrStampNameTooLong
	}
	text := trimSpaces(input.TextValue)
	if len([]rune(text)) > StampTextMaxLength {
		return DrawingStampInput{}, ErrStampTextTooLong
	}
	if text == "" && !hasImage {
		return DrawingStampInput{}, ErrStampContentRequired
	}
	priority := trimSpaces(input.Priority)
	if priority == "" {
		if hasImage {
			priority = StampPriorityImage
		} else {
			priority = StampPriorityText
		}
	}
	switch priority {
	case StampPriorityText:
		if text == "" {
			return DrawingStampInput{}, ErrStampPriorityInvalid
		}
	case StampPriorityImage:
		if !hasImage {
			return DrawingStampInput{}, ErrStampPriorityInvalid
		}
	default:
		return DrawingStampInput{}, ErrStampPriorityInvalid
	}
	return DrawingStampInput{Name: name, TextValue: text, Priority: priority}, nil
}
