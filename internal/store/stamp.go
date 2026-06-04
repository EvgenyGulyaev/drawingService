package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"drawingService/internal/model"

	bolt "go.etcd.io/bbolt"
)

func stampNameKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

var (
	DrawingStampsBucket        = []byte("DrawingStamps")
	DrawingStampDriveIDsBucket = []byte("DrawingStampDriveIDs")
)

func (r *DrawingRepository) ensureStampBuckets() error {
	if err := r.db.EnsureBucket(DrawingStampsBucket); err != nil {
		return err
	}
	return r.db.EnsureBucket(DrawingStampDriveIDsBucket)
}

func (r *DrawingRepository) CreateStamp(input model.DrawingStampInput, driveFileID string, size int64, mimeType string, width int, height int, actor string) (model.DrawingStamp, error) {
	hasImage := driveFileID != ""
	normalized, err := model.NormalizeStampInput(input, hasImage)
	if err != nil {
		return model.DrawingStamp{}, err
	}
	if hasImage && mimeType == "" {
		mimeType = model.DefaultMimeType
	}
	now := time.Now().UTC()
	stamp := model.DrawingStamp{
		Name:          normalized.Name,
		TextValue:     normalized.TextValue,
		HasImage:      hasImage,
		ImageMimeType: mimeType,
		ImageSize:     size,
		ImageWidth:    width,
		ImageHeight:   height,
		Priority:      normalized.Priority,
		CreatedBy:     actor,
		UpdatedBy:     actor,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	err = r.db.Update(func(tx *bolt.Tx) error {
		stamps := tx.Bucket(DrawingStampsBucket)
		drive := tx.Bucket(DrawingStampDriveIDsBucket)
		if stamps == nil || drive == nil {
			return fmt.Errorf("drawing stamp buckets not found")
		}
		seq, err := stamps.NextSequence()
		if err != nil {
			return err
		}
		stamp.ID = fmt.Sprintf(model.StampIDFormat, seq)
		data, err := json.Marshal(stamp)
		if err != nil {
			return err
		}
		if err := stamps.Put([]byte(stamp.ID), data); err != nil {
			return err
		}
		if hasImage {
			return drive.Put([]byte(stamp.ID), []byte(driveFileID))
		}
		return nil
	})
	if err != nil {
		return model.DrawingStamp{}, err
	}
	stamp.ImageDriveFileID = driveFileID
	return stamp, nil
}

func (r *DrawingRepository) ListStamps() ([]model.DrawingStamp, error) {
	result := make([]model.DrawingStamp, 0)
	err := r.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(DrawingStampsBucket)
		if b == nil {
			return fmt.Errorf("drawing stamps bucket not found")
		}
		cursor := b.Cursor()
		for key, value := cursor.Last(); key != nil; key, value = cursor.Prev() {
			var item model.DrawingStamp
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
	return result, nil
}

func (r *DrawingRepository) FindStamp(id string) (model.DrawingStamp, error) {
	if id == "" {
		return model.DrawingStamp{}, ErrNotFound
	}
	var item model.DrawingStamp
	err := r.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(DrawingStampsBucket)
		if b == nil {
			return fmt.Errorf("drawing stamps bucket not found")
		}
		raw := b.Get([]byte(id))
		if raw == nil {
			return ErrNotFound
		}
		return json.Unmarshal(raw, &item)
	})
	if err != nil {
		return model.DrawingStamp{}, err
	}
	return item, nil
}

func (r *DrawingRepository) stampDriveFileID(tx *bolt.Tx, id string) (string, error) {
	b := tx.Bucket(DrawingStampDriveIDsBucket)
	if b == nil {
		return "", fmt.Errorf("drawing stamp drive ids bucket not found")
	}
	raw := b.Get([]byte(id))
	if raw == nil {
		return "", ErrNotFound
	}
	return string(raw), nil
}

func (r *DrawingRepository) FindStampWithDriveID(id string) (model.DrawingStamp, error) {
	item, err := r.FindStamp(id)
	if err != nil {
		return model.DrawingStamp{}, err
	}
	if !item.HasImage {
		return item, nil
	}
	err = r.db.View(func(tx *bolt.Tx) error {
		driveID, err := r.stampDriveFileID(tx, id)
		if err != nil {
			return err
		}
		item.ImageDriveFileID = driveID
		return nil
	})
	if err != nil {
		return model.DrawingStamp{}, err
	}
	return item, nil
}

func (r *DrawingRepository) UpdateStamp(id string, input model.DrawingStampInput, driveFileID string, size int64, mimeType string, width int, height int, removeImage bool, actor string) (model.DrawingStamp, error) {
	var updated model.DrawingStamp
	err := r.db.Update(func(tx *bolt.Tx) error {
		stamps := tx.Bucket(DrawingStampsBucket)
		drive := tx.Bucket(DrawingStampDriveIDsBucket)
		if stamps == nil || drive == nil {
			return fmt.Errorf("drawing stamp buckets not found")
		}
		raw := stamps.Get([]byte(id))
		if raw == nil {
			return ErrNotFound
		}
		if err := json.Unmarshal(raw, &updated); err != nil {
			return err
		}
		nextHasImage := updated.HasImage
		if removeImage {
			nextHasImage = false
		}
		if driveFileID != "" {
			nextHasImage = true
		}
		normalized, err := model.NormalizeStampInput(input, nextHasImage)
		if err != nil {
			return err
		}
		updated.Name = normalized.Name
		updated.TextValue = normalized.TextValue
		updated.Priority = normalized.Priority
		if removeImage {
			updated.HasImage = false
			updated.ImageMimeType = ""
			updated.ImageSize = 0
			updated.ImageWidth = 0
			updated.ImageHeight = 0
			if err := drive.Delete([]byte(id)); err != nil {
				return err
			}
		}
		if driveFileID != "" {
			if mimeType == "" {
				mimeType = model.DefaultMimeType
			}
			updated.HasImage = true
			updated.ImageMimeType = mimeType
			updated.ImageSize = size
			updated.ImageWidth = width
			updated.ImageHeight = height
			if err := drive.Put([]byte(id), []byte(driveFileID)); err != nil {
				return err
			}
		}
		updated.UpdatedBy = actor
		updated.UpdatedAt = time.Now().UTC()
		data, err := json.Marshal(updated)
		if err != nil {
			return err
		}
		return stamps.Put([]byte(id), data)
	})
	if err != nil {
		return model.DrawingStamp{}, err
	}
	if updated.HasImage {
		withDrive, err := r.FindStampWithDriveID(id)
		if err != nil {
			return model.DrawingStamp{}, err
		}
		return withDrive, nil
	}
	return updated, nil
}

func (r *DrawingRepository) DeleteStamp(id string) (model.DrawingStamp, error) {
	var removed model.DrawingStamp
	var driveFileID string
	err := r.db.Update(func(tx *bolt.Tx) error {
		stamps := tx.Bucket(DrawingStampsBucket)
		drive := tx.Bucket(DrawingStampDriveIDsBucket)
		if stamps == nil || drive == nil {
			return fmt.Errorf("drawing stamp buckets not found")
		}
		raw := stamps.Get([]byte(id))
		if raw == nil {
			return ErrNotFound
		}
		if err := json.Unmarshal(raw, &removed); err != nil {
			return err
		}
		if removed.HasImage {
			driveFileID = string(drive.Get([]byte(id)))
		}
		if err := stamps.Delete([]byte(id)); err != nil {
			return err
		}
		return drive.Delete([]byte(id))
	})
	if err != nil {
		return model.DrawingStamp{}, err
	}
	removed.ImageDriveFileID = driveFileID
	return removed, nil
}

func (r *DrawingRepository) DeleteDuplicateStampsByName(name string, keepID string) ([]model.DrawingStamp, error) {
	key := stampNameKey(name)
	if key == "" {
		return nil, nil
	}
	removed := make([]model.DrawingStamp, 0)
	err := r.db.Update(func(tx *bolt.Tx) error {
		stamps := tx.Bucket(DrawingStampsBucket)
		drive := tx.Bucket(DrawingStampDriveIDsBucket)
		if stamps == nil || drive == nil {
			return fmt.Errorf("drawing stamp buckets not found")
		}
		toDelete := make([]string, 0)
		cursor := stamps.Cursor()
		for rawID, value := cursor.First(); rawID != nil; rawID, value = cursor.Next() {
			id := string(rawID)
			if id == keepID {
				continue
			}
			var item model.DrawingStamp
			if err := json.Unmarshal(value, &item); err != nil {
				return err
			}
			if stampNameKey(item.Name) != key {
				continue
			}
			if item.HasImage {
				item.ImageDriveFileID = string(drive.Get(rawID))
			}
			removed = append(removed, item)
			toDelete = append(toDelete, id)
		}
		for _, id := range toDelete {
			if err := stamps.Delete([]byte(id)); err != nil {
				return err
			}
			if err := drive.Delete([]byte(id)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return removed, nil
}
