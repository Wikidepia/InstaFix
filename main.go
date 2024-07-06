package main

import (
	"bytes"
	"flag"
	"instafix/handlers"
	scraper "instafix/handlers/scraper"
	"instafix/utils"
	"instafix/views"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ansrivas/fiberprometheus/v2"
	"github.com/cockroachdb/pebble"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/pprof"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/valyala/bytebufferpool"
)

func init() {
	scraper.InitDB()

	// Create static folder if not exists
	os.Mkdir("static", 0755)
}

func main() {
	listenAddr := flag.String("listen", "0.0.0.0:3000", "Address to listen on")
	gridCacheMaxFlag := flag.String("grid-cache-entries", "1024", "Maximum number of grid images to cache")
	remoteScraperAddr := flag.String("remote-scraper", "", "Remote scraper address")
	flag.Parse()

	app := fiber.New()

	recoverConfig := recover.ConfigDefault
	recoverConfig.EnableStackTrace = true
	app.Use(recover.New(recoverConfig))
	app.Use(pprof.New())

	// Initialize Prometheus
	prometheus := fiberprometheus.New("InstaFix")
	prometheus.RegisterAt(app, "/metrics")
	app.Use(prometheus.Middleware)

	// Close database when app closes
	defer scraper.DB.Close()

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

	// Evict cache every minute
	go func() {
		for {
			evictCache()
			time.Sleep(5 * time.Minute)
		}
	}()

	app.Get("/", func(c *fiber.Ctx) error {
		viewsBuf := bytebufferpool.Get()
		defer bytebufferpool.Put(viewsBuf)
		c.Set("Content-Type", "text/html; charset=utf-8")
		views.Home(viewsBuf)
		return c.Send(viewsBuf.Bytes())
	})

	app.Get("/p/:postID/", handlers.Embed())
	app.Get("/tv/:postID", handlers.Embed())
	app.Get("/reel/:postID", handlers.Embed())
	app.Get("/reels/:postID", handlers.Embed())
	app.Get("/stories/:username/:postID", handlers.Embed())
	app.Get("/p/:postID/:mediaNum", handlers.Embed())
	app.Get("/:username/p/:postID/", handlers.Embed())
	app.Get("/:username/p/:postID/:mediaNum", handlers.Embed())
	app.Get("/:username/reel/:postID", handlers.Embed())

	app.Get("/images/:postID/:mediaNum", handlers.Images())
	app.Get("/videos/:postID/:mediaNum", handlers.Videos())
	app.Get("/grid/:postID", handlers.Grid())
	app.Get("/oembed", handlers.OEmbed())

	if err := app.Listen(*listenAddr); err != nil {
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
