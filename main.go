package main

import (
	"instafix/handlers"
	data "instafix/handlers/data"
	"instafix/views"

	"github.com/davidbyttow/govips/v2/vips"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/pprof"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/template/pug/v2"
	"github.com/rs/zerolog"
	"github.com/valyala/fasthttp"
)

var client *fasthttp.Client

func init() {
	data.InitDB()
}

func main() {
	engine := pug.New("./views", ".pug")

	app := fiber.New(fiber.Config{
		Views: engine,
	})
	app.Use(recover.New())
	app.Use(pprof.New())

	// Initialize VIPS
	vips.Startup(nil)
	defer vips.Shutdown()

	// Close buntdb when app closes
	defer data.DB.Close()

	// Initialize zerolog
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.SetGlobalLevel(zerolog.ErrorLevel)

	app.Get("/", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/html")
		wr := c.Response().BodyWriter()
		views.Home(wr)
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

	app.Listen("127.0.0.1:3000")
}
