package store

import (
	bolt "go.etcd.io/bbolt"
)

type Db struct {
	filename string
	DB       *bolt.DB
}

func OpenDb(filename string) (*Db, error) {
	if filename == "" {
		filename = "drawing.db"
	}
	db, err := bolt.Open(filename, 0o600, nil)
	if err != nil {
		return nil, err
	}
	return &Db{filename: filename, DB: db}, nil
}

func (d *Db) Close() error {
	if d == nil || d.DB == nil {
		return nil
	}
	return d.DB.Close()
}

func (d *Db) EnsureBucket(name []byte) error {
	return d.DB.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(name)
		return err
	})
}

func (d *Db) Update(fn func(*bolt.Tx) error) error {
	return d.DB.Update(fn)
}

func (d *Db) View(fn func(*bolt.Tx) error) error {
	return d.DB.View(fn)
}
