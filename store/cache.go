// Package store handles all local persistence: the bbolt response cache
// and the static pre-built artist graph.
package store

import (
	"bytes"
	"encoding/gob"
	"time"

	bolt "go.etcd.io/bbolt"

	"pixeltui/lastfm"
)

const (
	bucketSimilarTracks  = "st"
	bucketTrackTags      = "tt"
	bucketSimilarArtists = "sa"
	bucketArtistTracks   = "at"

	ttlSimilarTracks  = 7 * 24 * time.Hour
	ttlTrackTags      = 30 * 24 * time.Hour
	ttlSimilarArtists = 7 * 24 * time.Hour
	ttlArtistTracks   = 24 * time.Hour
)

var allBuckets = []string{
	bucketSimilarTracks, bucketTrackTags, bucketSimilarArtists, bucketArtistTracks,
}

// cacheRecord wraps serialised data with a storage timestamp.
type cacheRecord struct {
	Data   []byte
	Stored int64 // unix seconds
}

// Cache is a bbolt-backed store for Last.fm API responses.
// All writes go through Put* methods; all reads through Get* methods.
// Reads return (value, true) on a fresh hit, (nil, false) on miss or expiry.
type Cache struct {
	db *bolt.DB
}

// OpenCache opens (or creates) the cache database at path.
func OpenCache(path string) (*Cache, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range allBuckets {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Cache{db: db}, nil
}

func (c *Cache) Close() error { return c.db.Close() }

// Stats returns the number of entries per bucket.
func (c *Cache) Stats() map[string]int {
	counts := make(map[string]int)
	c.db.View(func(tx *bolt.Tx) error {
		for _, name := range allBuckets {
			counts[name] = tx.Bucket([]byte(name)).Stats().KeyN
		}
		return nil
	})
	return counts
}

// Clear removes all entries from every bucket.
func (c *Cache) Clear() error {
	return c.db.Update(func(tx *bolt.Tx) error {
		for _, name := range allBuckets {
			if err := tx.DeleteBucket([]byte(name)); err != nil {
				return err
			}
			if _, err := tx.CreateBucket([]byte(name)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ── internal helpers ──────────────────────────────────────────────────────────

func gobEncode(v any) ([]byte, error) {
	var buf bytes.Buffer
	err := gob.NewEncoder(&buf).Encode(v)
	return buf.Bytes(), err
}

func (c *Cache) rawGet(bucket, key string, ttl time.Duration, allowStale bool) ([]byte, bool) {
	var payload []byte
	c.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket([]byte(bucket)).Get([]byte(key))
		if v != nil {
			payload = append([]byte(nil), v...) // copy out of bolt's buffer
		}
		return nil
	})
	if payload == nil {
		return nil, false
	}
	var rec cacheRecord
	if err := gob.NewDecoder(bytes.NewReader(payload)).Decode(&rec); err != nil {
		return nil, false
	}
	age := time.Since(time.Unix(rec.Stored, 0))
	if !allowStale && age > ttl {
		return nil, false
	}
	return rec.Data, true
}

func (c *Cache) rawPut(bucket, key string, data []byte) error {
	rec := cacheRecord{Data: data, Stored: time.Now().Unix()}
	encoded, err := gobEncode(rec)
	if err != nil {
		return err
	}
	return c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucket)).Put([]byte(key), encoded)
	})
}

// ── typed accessors ───────────────────────────────────────────────────────────

func (c *Cache) GetSimilarTracks(artist, track string, allowStale bool) ([]lastfm.SimilarTrack, bool) {
	raw, ok := c.rawGet(bucketSimilarTracks, artist+"|"+track, ttlSimilarTracks, allowStale)
	if !ok {
		return nil, false
	}
	var v []lastfm.SimilarTrack
	if err := gob.NewDecoder(bytes.NewReader(raw)).Decode(&v); err != nil {
		return nil, false
	}
	return v, true
}

func (c *Cache) PutSimilarTracks(artist, track string, v []lastfm.SimilarTrack) error {
	data, err := gobEncode(v)
	if err != nil {
		return err
	}
	return c.rawPut(bucketSimilarTracks, artist+"|"+track, data)
}

func (c *Cache) GetTrackTags(artist, track string, allowStale bool) ([]string, bool) {
	raw, ok := c.rawGet(bucketTrackTags, artist+"|"+track, ttlTrackTags, allowStale)
	if !ok {
		return nil, false
	}
	var v []string
	if err := gob.NewDecoder(bytes.NewReader(raw)).Decode(&v); err != nil {
		return nil, false
	}
	return v, true
}

func (c *Cache) PutTrackTags(artist, track string, v []string) error {
	data, err := gobEncode(v)
	if err != nil {
		return err
	}
	return c.rawPut(bucketTrackTags, artist+"|"+track, data)
}

func (c *Cache) GetSimilarArtists(artist string, allowStale bool) ([]lastfm.SimilarArtist, bool) {
	raw, ok := c.rawGet(bucketSimilarArtists, artist, ttlSimilarArtists, allowStale)
	if !ok {
		return nil, false
	}
	var v []lastfm.SimilarArtist
	if err := gob.NewDecoder(bytes.NewReader(raw)).Decode(&v); err != nil {
		return nil, false
	}
	return v, true
}

func (c *Cache) PutSimilarArtists(artist string, v []lastfm.SimilarArtist) error {
	data, err := gobEncode(v)
	if err != nil {
		return err
	}
	return c.rawPut(bucketSimilarArtists, artist, data)
}

func (c *Cache) GetArtistTopTracks(artist string, allowStale bool) ([]lastfm.TopTrack, bool) {
	raw, ok := c.rawGet(bucketArtistTracks, artist, ttlArtistTracks, allowStale)
	if !ok {
		return nil, false
	}
	var v []lastfm.TopTrack
	if err := gob.NewDecoder(bytes.NewReader(raw)).Decode(&v); err != nil {
		return nil, false
	}
	return v, true
}

func (c *Cache) PutArtistTopTracks(artist string, v []lastfm.TopTrack) error {
	data, err := gobEncode(v)
	if err != nil {
		return err
	}
	return c.rawPut(bucketArtistTracks, artist, data)
}
