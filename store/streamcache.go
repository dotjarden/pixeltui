package store

import (
	"bytes"
	"encoding/gob"
	"time"

	bolt "go.etcd.io/bbolt"
)

// Resolved-stream-URL cache. YouTube CDN (googlevideo) URLs carry an `expire`
// unix timestamp; we cache videoID → {url, expire} so replays and restarts skip
// the slow yt-dlp resolution entirely until the URL actually expires.

const bucketStreamURL = "su"

type streamRec struct {
	URL    string
	Expire int64 // unix seconds; 0 = unknown
}

// GetStreamURL returns a cached, still-valid CDN URL for videoID.
// Returns ("", false) on miss or if the URL is within 60s of expiry.
func (c *Cache) GetStreamURL(videoID string) (string, bool) {
	if c == nil || videoID == "" {
		return "", false
	}
	var rec streamRec
	found := false
	c.db.View(func(tx *bolt.Tx) error { //nolint:errcheck
		b := tx.Bucket([]byte(bucketStreamURL))
		if b == nil {
			return nil
		}
		v := b.Get([]byte(videoID))
		if v == nil {
			return nil
		}
		if err := gob.NewDecoder(bytes.NewReader(v)).Decode(&rec); err == nil {
			found = true
		}
		return nil
	})
	if !found || rec.URL == "" {
		return "", false
	}
	if rec.Expire != 0 && time.Now().Unix() > rec.Expire-60 {
		return "", false // expired (or about to)
	}
	return rec.URL, true
}

// PutStreamURL caches a resolved CDN URL for videoID until `expire`.
func (c *Cache) PutStreamURL(videoID, url string, expire int64) {
	if c == nil || videoID == "" || url == "" {
		return
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(streamRec{URL: url, Expire: expire}); err != nil {
		return
	}
	c.db.Update(func(tx *bolt.Tx) error { //nolint:errcheck
		b, err := tx.CreateBucketIfNotExists([]byte(bucketStreamURL))
		if err != nil {
			return err
		}
		return b.Put([]byte(videoID), buf.Bytes())
	})
}
