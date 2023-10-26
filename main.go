package main

import (
	"instafix/handlers"
	data "instafix/handlers/data"
	"instafix/views"

	"github.com/ansrivas/fiberprometheus/v2"
	"github.com/davidbyttow/govips/v2/vips"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/pprof"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/rs/zerolog"
	"github.com/valyala/bytebufferpool"
)

func init() {
	data.InitDB()
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

	// Initialize VIPS
	vips.Startup(nil)
	defer vips.Shutdown()

	// Close buntdb when app closes
	defer data.DB.Close()

	// Initialize zerolog
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.SetGlobalLevel(zerolog.ErrorLevel)

	app.Get("/", func(c *fiber.Ctx) error {
		viewsBuf := bytebufferpool.Get()
		defer bytebufferpool.Put(viewsBuf)
		c.Set("Content-Type", "text/html; charset=utf-8")
		views.Home(viewsBuf)
		c.Response().SetBodyRaw(viewsBuf.Bytes())
		return nil
	})

	// GET /p/Cw8X2wXPjiM
	app.Get("/p/:postID/", handlers.Embed())
	app.Get("/tv/:postID", handlers.Embed())
	app.Get("/reel/:postID", handlers.Embed())
	app.Get("/reels/:postID", handlers.Embed())
	app.Get("/p/:postID/:mediaNum", handlers.Embed())
	app.Get("/images/:postID/:mediaNum", handlers.Images())
	app.Get("/videos/:postID/:mediaNum", handlers.Videos())
	app.Get("/grid/:postID", handlers.Grid())
	app.Get("/oembed", handlers.OEmbed())

	app.Listen("127.0.0.1:3000")
}
