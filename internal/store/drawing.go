package store

import (
	"drawingService/internal/model"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	bolt "go.etcd.io/bbolt"
)

var DrawingImagesBucket = []byte("DrawingImages")

var ErrNotFound = errors.New("drawing image not found")

type DrawingRepository struct {
	db *Db
}

func NewDrawingRepository(db *Db) *DrawingRepository {
	return &DrawingRepository{db: db}
}

func (r *DrawingRepository) EnsureBuckets() error {
	return r.db.EnsureBucket(DrawingImagesBucket)
}

func (r *DrawingRepository) Create(input model.DrawingImageInput, driveFileID string, size int64, mimeType string, actor string) (model.DrawingImage, error) {
	title, err := model.NormalizeTitle(input.Title)
	if err != nil {
		return model.DrawingImage{}, err
	}
	if mimeType == "" {
		mimeType = model.DefaultMimeType
	}
	if size <= 0 {
		return model.DrawingImage{}, errors.New("size must be positive")
	}
	if input.Width <= 0 || input.Height <= 0 {
		return model.DrawingImage{}, errors.New("width and height are required")
	}

	now := time.Now().UTC()
	image := model.DrawingImage{
		Title:       title,
		DriveFileID: driveFileID,
		MimeType:    mimeType,
		Size:        size,
		Width:       input.Width,
		Height:      input.Height,
		CreatedBy:   actor,
		UpdatedBy:   actor,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err = r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(DrawingImagesBucket)
		if b == nil {
			return fmt.Errorf("drawing images bucket not found")
		}
		seq, err := b.NextSequence()
		if err != nil {
			return err
		}
		image.ID = fmt.Sprintf(model.DrawingIDFormat, seq)
		data, err := json.Marshal(image)
		if err != nil {
			return err
		}
		return b.Put([]byte(image.ID), data)
	})
	if err != nil {
		return model.DrawingImage{}, err
	}
	return image, nil
}

func (r *DrawingRepository) List() ([]model.DrawingImage, error) {
	result := make([]model.DrawingImage, 0)
	err := r.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(DrawingImagesBucket)
		if b == nil {
			return fmt.Errorf("drawing images bucket not found")
		}
		cursor := b.Cursor()
		for key, value := cursor.Last(); key != nil; key, value = cursor.Prev() {
			var item model.DrawingImage
			if err := json.Unmarshal(value, &item); err != nil {
				return err
			}
			result = append(result, item)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

func (r *DrawingRepository) Find(id string) (model.DrawingImage, error) {
	if id == "" {
		return model.DrawingImage{}, ErrNotFound
	}
	var item model.DrawingImage
	err := r.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(DrawingImagesBucket)
		if b == nil {
			return fmt.Errorf("drawing images bucket not found")
		}
		raw := b.Get([]byte(id))
		if raw == nil {
			return ErrNotFound
		}
		return json.Unmarshal(raw, &item)
	})
	if err != nil {
		return model.DrawingImage{}, err
	}
	return item, nil
}

func (r *DrawingRepository) Update(id string, input model.DrawingImageInput, size int64, mimeType string, actor string) (model.DrawingImage, error) {
	title, err := model.NormalizeTitle(input.Title)
	if err != nil {
		return model.DrawingImage{}, err
	}
	if mimeType == "" {
		mimeType = model.DefaultMimeType
	}
	if size <= 0 {
		return model.DrawingImage{}, errors.New("size must be positive")
	}

	var updated model.DrawingImage
	err = r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(DrawingImagesBucket)
		if b == nil {
			return fmt.Errorf("drawing images bucket not found")
		}
		raw := b.Get([]byte(id))
		if raw == nil {
			return ErrNotFound
		}
		if err := json.Unmarshal(raw, &updated); err != nil {
			return err
		}
		updated.Title = title
		updated.MimeType = mimeType
		updated.Size = size
		if input.Width > 0 {
			updated.Width = input.Width
		}
		if input.Height > 0 {
			updated.Height = input.Height
		}
		updated.UpdatedBy = actor
		updated.UpdatedAt = time.Now().UTC()
		data, err := json.Marshal(updated)
		if err != nil {
			return err
		}
		return b.Put([]byte(id), data)
	})
	if err != nil {
		return model.DrawingImage{}, err
	}
	return updated, nil
}

func (r *DrawingRepository) Delete(id string) (model.DrawingImage, error) {
	var removed model.DrawingImage
	err := r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(DrawingImagesBucket)
		if b == nil {
			return fmt.Errorf("drawing images bucket not found")
		}
		raw := b.Get([]byte(id))
		if raw == nil {
			return ErrNotFound
		}
		if err := json.Unmarshal(raw, &removed); err != nil {
			return err
		}
		return b.Delete([]byte(id))
	})
	if err != nil {
		return model.DrawingImage{}, err
	}
	return removed, nil
}
