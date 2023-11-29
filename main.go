package main

import (
	"flag"
	"instafix/handlers"
	data "instafix/handlers/data"
	"instafix/views"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ansrivas/fiberprometheus/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/pprof"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/valyala/bytebufferpool"
)

func byteSizeStrToInt(n string) (int64, error) {
	sizeStr := strings.ToLower(n)
	sizeStr = strings.TrimSpace(sizeStr)

	units := map[string]int64{
		"kb": 1024,
		"mb": 1024 * 1024,
		"gb": 1024 * 1024 * 1024,
		"tb": 1024 * 1024 * 1024 * 1024,
	}

	for unit, multiplier := range units {
		if strings.HasSuffix(sizeStr, unit) {
			sizeStr = strings.TrimSuffix(sizeStr, unit)
			sizeStr = strings.TrimSpace(sizeStr)

			size, err := strconv.ParseInt(sizeStr, 10, 64)
			if err != nil {
				return -1, err
			}
			return size * multiplier, nil
		}
	}
	return -1, nil
}

func init() {
	data.InitDB()

	// Create static folder if not exists
	os.Mkdir("static", 0755)
}

func main() {
	listenAddr := flag.String("listen", "0.0.0.0:3000", "Address to listen on")
	gridCacheSize := flag.String("grid-cache-size", "25GB", "Grid cache size")
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

	// Close buntdb when app closes
	defer data.DB.Close()

	// Initialize zerolog
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.SetGlobalLevel(zerolog.WarnLevel)

	// Parse grid-cache-size
	gridCacheSizeParsed, err := byteSizeStrToInt(*gridCacheSize)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to parse grid-cache-size")
	}

	// Evict static files when above threshold
	go func() {
		for {
			evictStatic(gridCacheSizeParsed)
			time.Sleep(5 * time.Minute) // 5 min delay
		}
	}()

	app.Get("/", func(c *fiber.Ctx) error {
		viewsBuf := bytebufferpool.Get()
		defer bytebufferpool.Put(viewsBuf)
		c.Set("Content-Type", "text/html; charset=utf-8")
		views.Home(viewsBuf)
		return c.Send(viewsBuf.Bytes())
	})

	// GET /p/Cw8X2wXPjiM
	// GET /stories/fatfatpankocat/3226148677671954631/
	app.Get("/p/:postID/", handlers.Embed())
	app.Get("/tv/:postID", handlers.Embed())
	app.Get("/reel/:postID", handlers.Embed())
	app.Get("/reels/:postID", handlers.Embed())
	app.Get("/stories/:username/:postID", handlers.Embed())
	app.Get("/p/:postID/:mediaNum", handlers.Embed())
	app.Get("/images/:postID/:mediaNum", handlers.Images())
	app.Get("/videos/:postID/:mediaNum", handlers.Videos())
	app.Get("/grid/:postID", handlers.Grid())
	app.Get("/oembed", handlers.OEmbed())

	app.Listen(*listenAddr)
}

// Remove file in static folder until below threshold
func evictStatic(threshold int64) {
	var dirSize int64 = 0
	readSize := func(path string, file os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !file.IsDir() {
			if dirSize > threshold {
				err := os.Remove(path)
				return err
			}
			dirSize += file.Size()
		}
		return nil
	}
	filepath.Walk("static", readSize)
}
