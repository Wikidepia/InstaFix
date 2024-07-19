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
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
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
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
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
		http.ListenAndServe("localhost:6060", nil)
	}()

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.StripSlashes)

	r.Get("/tv/{postID}", handlers.Embed)
	r.Get("/reel/{postID}", handlers.Embed)
	r.Get("/reels/{postID}", handlers.Embed)
	r.Get("/stories/{username}/{postID}", handlers.Embed)
	r.Get("/p/{postID}", handlers.Embed)
	r.Get("/p/{postID}/{mediaNum}", handlers.Embed)

	r.Get("/{username}/p/{postID}", handlers.Embed)
	r.Get("/{username}/p/{postID}/{mediaNum}", handlers.Embed)
	r.Get("/{username}/reel/{postID}", handlers.Embed)

	r.Get("/images/{postID}/{mediaNum}", handlers.Images)
	r.Get("/videos/{postID}/{mediaNum}", handlers.Videos)
	r.Get("/grid/{postID}", handlers.Grid)
	r.Get("/oembed", handlers.OEmbed)
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		views.Home(w)
	})
	if err := http.ListenAndServe(*listenAddr, r); err != nil {
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
