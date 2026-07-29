package storage

import (
	"encoding/json"
	"errors"
	"fmt"

	"go.etcd.io/bbolt"
)

var ErrNotFound = errors.New("key not found")

type DB struct{ db *bbolt.DB }

func Decode(data []byte, value interface{}) error {
	return json.Unmarshal(data, value)
}

func Open(path string) (*DB, error) {
	db, err := bbolt.Open(path, 0600, nil)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return &DB{db: db}, nil
}
func (d *DB) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	return d.db.Close()
}
func (d *DB) Get(bucket, key string) ([]byte, error) {
	var out []byte
	err := d.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return ErrNotFound
		}
		v := b.Get([]byte(key))
		if v == nil {
			return ErrNotFound
		}
		out = append([]byte(nil), v...)
		return nil
	})
	return out, err
}
func (d *DB) Set(bucket, key string, value interface{}) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return d.db.Update(func(tx *bbolt.Tx) error {
		bk, err := tx.CreateBucketIfNotExists([]byte(bucket))
		if err != nil {
			return err
		}
		return bk.Put([]byte(key), b)
	})
}
func (d *DB) Delete(bucket, key string) error {
	return d.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		return b.Delete([]byte(key))
	})
}
func (d *DB) List(bucket string) ([]string, error) {
	var keys []string
	err := d.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			if v != nil {
				keys = append(keys, string(k))
			}
			return nil
		})
	})
	return keys, err
}
