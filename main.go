package main

import (
	"flag"
	"instafix/handlers"
	scraper "instafix/handlers/scraper"
	"instafix/utils"
	"instafix/views"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	bolt "go.etcd.io/bbolt"
)

func init() {
	// Create static folder if not exists
	os.Mkdir("static", 0755)
}

func main() {
	listenAddr := flag.String("listen", "0.0.0.0:3000", "Address to listen on")
	gridCacheMaxFlag := flag.String("grid-cache-entries", "1024", "Maximum number of grid images to cache")
	remoteScraperAddr := flag.String("remote-scraper", "", "Remote scraper address (https://github.com/Wikidepia/InstaFix-remote-scraper)")
	videoProxyAddr := flag.String("video-proxy-addr", "", "Video proxy address (https://github.com/Wikidepia/InstaFix-proxy)")
	flag.Parse()

	// Initialize remote scraper
	if *remoteScraperAddr != "" {
		if !strings.HasPrefix(*remoteScraperAddr, "http") {
			panic("Remote scraper address must start with http:// or https://")
		}
		scraper.RemoteScraperAddr = *remoteScraperAddr
	}

	// Initialize video proxy
	if *videoProxyAddr != "" {
		if !strings.HasPrefix(*videoProxyAddr, "http") {
			panic("Video proxy address must start with http:// or https://")
		}
		handlers.VideoProxyAddr = *videoProxyAddr
		if !strings.HasSuffix(handlers.VideoProxyAddr, "/") {
			handlers.VideoProxyAddr += "/"
		}
	}

	// Initialize logging
	slog.SetLogLoggerLevel(slog.LevelError)

	// Initialize LRU
	gridCacheMax, err := strconv.Atoi(*gridCacheMaxFlag)
	if err != nil || gridCacheMax <= 0 {
		panic(err)
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

	println("Starting up on", *listenAddr)
	if err := http.ListenAndServe(*listenAddr, r); err != nil {
		slog.Error("Failed to listen", "err", err)
	}
}

// Remove cache from Pebble if already expired
func evictCache() {
	curTime := time.Now().UnixNano()
	err := scraper.DB.Batch(func(tx *bolt.Tx) error {
		ttlBucket := tx.Bucket([]byte("ttl"))
		if ttlBucket == nil {
			return nil
		}
		dataBucket := tx.Bucket([]byte("data"))
		if dataBucket == nil {
			return nil
		}
		c := ttlBucket.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if n, err := strconv.ParseInt(utils.B2S(k), 10, 64); err == nil {
				if n < curTime {
					ttlBucket.Delete(k)
					dataBucket.Delete(v)
				}
			} else {
				slog.Error("Failed to parse expire timestamp in cache", "err", err)
			}
		}
		return nil
	})
	if err != nil {
		slog.Error("Failed to evict cache", "err", err)
	}
}
