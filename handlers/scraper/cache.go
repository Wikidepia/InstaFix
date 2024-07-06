package handlers

import (
	"os"

	"github.com/cespare/xxhash/v2"
	"github.com/cockroachdb/pebble"
	"github.com/elastic/go-freelru"
	"github.com/rs/zerolog/log"
)

var DB *pebble.DB
var LRU *freelru.SyncedLRU[string, bool]

func hashStringXXHASH(s string) uint32 {
	return uint32(xxhash.Sum64String(s))
}

func InitDB() {
	db, err := pebble.Open("database", &pebble.Options{})

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
		log.Fatal().Err(err).Msg("Failed to read static folder")
	}
	for _, d := range dir {
		if !d.IsDir() {
			lru.Add("static/"+d.Name(), true)
		}
	}

	LRU = lru
}
