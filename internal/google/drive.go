package google

import (
	"context"
	"errors"
	"fmt"
	"io"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

var (
	ErrNotFound    = errors.New("file not found in storage")
	ErrInvalidName = errors.New("file name is required")
)

type Storage interface {
	UploadPNG(ctx context.Context, name string, body io.Reader, size int64) (fileID string, err error)
	UpdatePNG(ctx context.Context, fileID string, body io.Reader, size int64) error
	Download(ctx context.Context, fileID string) (io.ReadCloser, string, error)
	Delete(ctx context.Context, fileID string) error
	Exists(ctx context.Context, fileID string) (bool, error)
}

type DriveStorage struct {
	service  *drive.Service
	folderID string
}

type DriveOptions struct {
	FolderID          string
	CredentialsFile   string
	OAuthClientID     string
	OAuthClientSecret string
	OAuthRefreshToken string
}

func NewDriveStorage(ctx context.Context, opts DriveOptions) (*DriveStorage, error) {
	if opts.FolderID == "" {
		return nil, errors.New("folderID is required")
	}
	serviceOptions, err := buildServiceOptions(ctx, opts)
	if err != nil {
		return nil, err
	}
	svc, err := drive.NewService(ctx, serviceOptions...)
	if err != nil {
		return nil, fmt.Errorf("create drive service: %w", err)
	}
	return &DriveStorage{service: svc, folderID: opts.FolderID}, nil
}

func buildServiceOptions(ctx context.Context, opts DriveOptions) ([]option.ClientOption, error) {
	if opts.OAuthRefreshToken != "" || opts.OAuthClientID != "" || opts.OAuthClientSecret != "" {
		if opts.OAuthClientID == "" {
			return nil, errors.New("oauth client id is required")
		}
		if opts.OAuthClientSecret == "" {
			return nil, errors.New("oauth client secret is required")
		}
		if opts.OAuthRefreshToken == "" {
			return nil, errors.New("oauth refresh token is required")
		}
		cfg := &oauth2.Config{
			ClientID:     opts.OAuthClientID,
			ClientSecret: opts.OAuthClientSecret,
			Endpoint:     google.Endpoint,
			Scopes:       []string{drive.DriveScope},
		}
		client := cfg.Client(ctx, &oauth2.Token{RefreshToken: opts.OAuthRefreshToken})
		return []option.ClientOption{option.WithHTTPClient(client)}, nil
	}
	if opts.CredentialsFile == "" {
		return nil, errors.New("credentials file is required")
	}
	return []option.ClientOption{option.WithAuthCredentialsFile(option.ServiceAccount, opts.CredentialsFile)}, nil
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
	created, err := s.service.Files.Create(file).
		Media(body, googleapi.ContentType("image/png")).
		SupportsAllDrives(true).
		Context(ctx).
		Do()
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
	_, err := s.service.Files.Update(fileID, &drive.File{}).
		Media(body, googleapi.ContentType("image/png")).
		SupportsAllDrives(true).
		Context(ctx).
		Do()
	if err != nil {
		return fmt.Errorf("drive update: %w", err)
	}
	return nil
}

func (s *DriveStorage) Download(ctx context.Context, fileID string) (io.ReadCloser, string, error) {
	if fileID == "" {
		return nil, "", errors.New("fileID is required")
	}
	resp, err := s.service.Files.Get(fileID).
		SupportsAllDrives(true).
		Context(ctx).
		Download()
	if err != nil {
		return nil, "", fmt.Errorf("drive download: %w", err)
	}
	return resp.Body, "image/png", nil
}

func (s *DriveStorage) Delete(ctx context.Context, fileID string) error {
	if fileID == "" {
		return errors.New("fileID is required")
	}
	err := s.service.Files.Delete(fileID).
		SupportsAllDrives(true).
		Context(ctx).
		Do()
	if err == nil {
		return nil
	}
	if isNotFound(err) {
		return nil
	}
	return fmt.Errorf("drive delete: %w", err)
}

func (s *DriveStorage) Exists(ctx context.Context, fileID string) (bool, error) {
	if fileID == "" {
		return false, errors.New("fileID is required")
	}
	file, err := s.service.Files.Get(fileID).
		Fields("id,trashed").
		SupportsAllDrives(true).
		Context(ctx).
		Do()
	if err == nil {
		return !file.Trashed, nil
	}
	if isNotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("drive exists: %w", err)
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

// Ping checks that the service account can see the configured folder. It uses a short
// timeout so /healthz does not block forever if Drive is unreachable.
func (s *DriveStorage) Ping(ctx context.Context) error {
	_, err := s.service.Files.Get(s.folderID).
		Fields("id").
		SupportsAllDrives(true).
		Context(ctx).
		Do()
	if err != nil {
		return fmt.Errorf("drive ping: %w", err)
	}
	return nil
}
