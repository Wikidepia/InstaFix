package main

import (
	"bytes"
	"flag"
	"instafix/handlers"
	scraper "instafix/handlers/scraper"
	"instafix/utils"
	"instafix/views"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func init() {
	// Create static folder if not exists
	os.Mkdir("static", 0755)
}

func main() {
	listenAddr := flag.String("listen", "0.0.0.0:3000", "Address to listen on")
	gridCacheMaxFlag := flag.String("grid-cache-entries", "1024", "Maximum number of grid images to cache")
	remoteScraperAddr := flag.String("remote-scraper", "", "Remote scraper address")
	flag.Parse()

	// Initialize remote scraper
	if *remoteScraperAddr != "" {
		if !strings.HasPrefix(*remoteScraperAddr, "http") {
			log.Fatal().Msg("Invalid remote scraper address")
		}
		scraper.RemoteScraperAddr = *remoteScraperAddr
	}

	// Initialize zerolog
	zerolog.SetGlobalLevel(zerolog.ErrorLevel)

	// Initialize LRU
	gridCacheMax, err := strconv.Atoi(*gridCacheMaxFlag)
	if err != nil || gridCacheMax <= 0 {
		log.Fatal().Err(err).Msg("Failed to parse grid-cache-entries or invalid value")
	}
	scraper.InitLRU(gridCacheMax)

	// Initialize cache / DB
	scraper.InitDB()
	defer scraper.DB.Close()

	// Evict cache every minute
	go func() {
		for {
			evictCache()
			time.Sleep(5 * time.Minute)
		}
	}()

	go func() {
		log.Info().Msg("Starting pprof server")
		http.ListenAndServe("0.0.0.0:6060", nil)
	}()

	mux := http.NewServeMux()
	// mux.HandleFunc("GET /{username}/p/{postID}", handlers.Embed)
	// mux.HandleFunc("GET /{username}/p/{postID}/{mediaNum}", handlers.Embed)
	// mux.HandleFunc("GET /{username}/reel/{postID}", handlers.Embed)

	mux.HandleFunc("GET /tv/{postID}/", handlers.Embed)
	mux.HandleFunc("GET /reel/{postID}/", handlers.Embed)
	mux.HandleFunc("GET /reels/{postID}/", handlers.Embed)
	mux.HandleFunc("GET /stories/{username}/{postID}/", handlers.Embed)
	mux.HandleFunc("GET /p/{postID}/", handlers.Embed)
	mux.HandleFunc("GET /p/{postID}/{mediaNum}/", handlers.Embed)

	mux.HandleFunc("/images/{postID}/{mediaNum}/", handlers.Images)
	mux.HandleFunc("/videos/{postID}/{mediaNum}/", handlers.Videos)
	mux.HandleFunc("/grid/{postID}/", handlers.Grid)
	mux.HandleFunc("/oembed/", handlers.OEmbed)
	mux.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		views.Home(w)
	})
	if err := http.ListenAndServe(*listenAddr, mux); err != nil {
		log.Fatal().Err(err).Msg("Failed to listen")
	}
}

// Remove cache from Pebble if already expired
func evictCache() {
	iter, err := scraper.DB.NewIter(&pebble.IterOptions{LowerBound: []byte("exp-")})
	if err != nil {
		log.Error().Err(err).Msg("Failed to create iterator when evicting cache")
		return
	}
	defer iter.Close()

	batch := scraper.DB.NewBatch()
	curTime := time.Now().UnixNano()
	for iter.First(); iter.Valid(); iter.Next() {
		if !bytes.HasPrefix(iter.Key(), []byte("exp-")) {
			continue
		}

		expireTimestamp := bytes.Trim(iter.Key(), "exp-")
		if n, err := strconv.ParseInt(utils.B2S(expireTimestamp), 10, 64); err == nil {
			if n < curTime {
				batch.Delete(iter.Key(), pebble.NoSync)
				batch.Delete(iter.Value(), pebble.NoSync)
			}
		} else {
			log.Error().Err(err).Msg("Failed to parse expire timestamp in cache")
		}
	}
	if err := batch.Commit(pebble.NoSync); err != nil {
		log.Error().Err(err).Msg("Failed to write when evicting cache")
	}
}
