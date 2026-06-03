package google

import (
	"context"
	"errors"
	"fmt"
	"io"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

var (
	ErrNotFound     = errors.New("file not found in storage")
	ErrInvalidName  = errors.New("file name is required")
)

type Storage interface {
	UploadPNG(ctx context.Context, name string, body io.Reader, size int64) (fileID string, err error)
	UpdatePNG(ctx context.Context, fileID string, body io.Reader, size int64) error
	Download(ctx context.Context, fileID string) (io.ReadCloser, string, error)
	Delete(ctx context.Context, fileID string) error
}

type DriveStorage struct {
	service  *drive.Service
	folderID string
}

func NewDriveStorage(ctx context.Context, credentialsFile, folderID string) (*DriveStorage, error) {
	if folderID == "" {
		return nil, errors.New("folderID is required")
	}
	if credentialsFile == "" {
		return nil, errors.New("credentials file is required")
	}
	svc, err := drive.NewService(ctx, option.WithCredentialsFile(credentialsFile))
	if err != nil {
		return nil, fmt.Errorf("create drive service: %w", err)
	}
	return &DriveStorage{service: svc, folderID: folderID}, nil
}

func NewDriveStorageFromService(svc *drive.Service, folderID string) *DriveStorage {
	return &DriveStorage{service: svc, folderID: folderID}
}

func (s *DriveStorage) UploadPNG(ctx context.Context, name string, body io.Reader, size int64) (string, error) {
	if name == "" {
		return "", ErrInvalidName
	}
	if body == nil {
		return "", errors.New("body is required")
	}
	if size <= 0 {
		return "", errors.New("size must be positive")
	}
	file := &drive.File{
		Name:     name,
		MimeType: "image/png",
		Parents:  []string{s.folderID},
	}
	created, err := s.service.Files.Create(file).Media(body, googleapi.ContentType("image/png")).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("drive create: %w", err)
	}
	return created.Id, nil
}

func (s *DriveStorage) UpdatePNG(ctx context.Context, fileID string, body io.Reader, size int64) error {
	if fileID == "" {
		return errors.New("fileID is required")
	}
	if body == nil {
		return errors.New("body is required")
	}
	if size <= 0 {
		return errors.New("size must be positive")
	}
	_, err := s.service.Files.Update(fileID, &drive.File{}).Media(body, googleapi.ContentType("image/png")).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("drive update: %w", err)
	}
	return nil
}

func (s *DriveStorage) Download(ctx context.Context, fileID string) (io.ReadCloser, string, error) {
	if fileID == "" {
		return nil, "", errors.New("fileID is required")
	}
	resp, err := s.service.Files.Get(fileID).Context(ctx).Download()
	if err != nil {
		return nil, "", fmt.Errorf("drive download: %w", err)
	}
	return resp.Body, "image/png", nil
}

func (s *DriveStorage) Delete(ctx context.Context, fileID string) error {
	if fileID == "" {
		return errors.New("fileID is required")
	}
	err := s.service.Files.Delete(fileID).Context(ctx).Do()
	if err == nil {
		return nil
	}
	if isNotFound(err) {
		return nil
	}
	return fmt.Errorf("drive delete: %w", err)
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code == 404
	}
	return false
}
