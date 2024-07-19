package handlers

import (
	"os"

	"github.com/cespare/xxhash/v2"
	"github.com/elastic/go-freelru"
	bolt "go.etcd.io/bbolt"
)

var DB *bolt.DB
var LRU *freelru.SyncedLRU[string, bool]

func hashStringXXHASH(s string) uint32 {
	return uint32(xxhash.Sum64String(s))
}

func InitDB() {
	db, err := bolt.Open("cache.db", 0600, nil)
	if err != nil {
		panic(err)
	}

	// Create buckets
	err = db.Update(func(tx *bolt.Tx) error {
		tx.CreateBucketIfNotExists([]byte("data"))
		tx.CreateBucketIfNotExists([]byte("ttl"))
		return nil
	})
	if err != nil {
		panic(err)
	}

	DB = db
}

func InitLRU(maxEntries int) {
	// Initialize LRU for grid caching
	lru, err := freelru.NewSynced[string, bool](uint32(maxEntries), hashStringXXHASH)
	if err != nil {
		panic(err)
	}

	lru.SetOnEvict(func(key string, value bool) {
		os.Remove(key)
	})

	// Fill LRU with existing files
	dir, err := os.ReadDir("static")
	if err != nil {
		panic(err)
	}
	for _, d := range dir {
		if !d.IsDir() {
			lru.Add("static/"+d.Name(), true)
		}
	}

	LRU = lru
}
