package main

import (
	"instafix/handlers"
	data "instafix/handlers/data"
	"instafix/views"
	"os"
	"path/filepath"
	"time"

	"github.com/ansrivas/fiberprometheus/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/pprof"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/rs/zerolog"
	"github.com/valyala/bytebufferpool"
)

func init() {
	data.InitDB()

	// Run evictStatic every 5 minutes
	go func() {
		for {
			evictStatic(25 * 1024 * 1024 * 1024) // 25 GB
			// Sleep for 5 minutes
			time.Sleep(time.Minute * 5)
		}
	}()
}

func main() {
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

	app.Listen("127.0.0.1:3000")
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
