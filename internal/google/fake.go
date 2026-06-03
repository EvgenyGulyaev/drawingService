package google

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
)

type FakeStorage struct {
	mu          sync.Mutex
	files       map[string][]byte
	pingErr     error
	uploadErr   error
	updateErr   error
	downloadErr error
	deleteErr   error
	nextID      int
}

func NewFakeStorage() *FakeStorage {
	return &FakeStorage{files: map[string][]byte{}, nextID: 1}
}

func (f *FakeStorage) UploadPNG(_ context.Context, name string, body io.Reader, size int64) (string, error) {
	if f.uploadErr != nil {
		return "", f.uploadErr
	}
	if name == "" {
		return "", ErrInvalidName
	}
	if body == nil {
		return "", errors.New("body is required")
	}
	if size <= 0 {
		return "", errors.New("size must be positive")
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.nextID
	f.nextID++
	fileID := fmtID(id)
	f.files[fileID] = data
	return fileID, nil
}

func (f *FakeStorage) UpdatePNG(_ context.Context, fileID string, body io.Reader, size int64) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	if fileID == "" {
		return errors.New("fileID is required")
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if size > 0 && int64(len(data)) != size {
		return errors.New("size mismatch")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.files[fileID]; !ok {
		return ErrNotFound
	}
	f.files[fileID] = data
	return nil
}

func (f *FakeStorage) Download(_ context.Context, fileID string) (io.ReadCloser, string, error) {
	if f.downloadErr != nil {
		return nil, "", f.downloadErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.files[fileID]
	if !ok {
		return nil, "", ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), "image/png", nil
}

func (f *FakeStorage) Delete(_ context.Context, fileID string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.files[fileID]; !ok {
		return nil
	}
	delete(f.files, fileID)
	return nil
}

func (f *FakeStorage) HasFile(fileID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.files[fileID]
	return ok
}

func fmtID(n int) string {
	return "drive-" + intToStr(n)
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func (f *FakeStorage) SetUploadError(err error)   { f.uploadErr = err }
func (f *FakeStorage) SetUpdateError(err error)   { f.updateErr = err }
func (f *FakeStorage) SetDownloadError(err error) { f.downloadErr = err }
func (f *FakeStorage) SetDeleteError(err error)   { f.deleteErr = err }

func (f *FakeStorage) Ping(_ context.Context) error {
	return f.pingErr
}

func (f *FakeStorage) SetPingError(err error) { f.pingErr = err }
