package handlers

import (
	"instafix/views"
	"instafix/views/model"

	"github.com/PurpleSec/escape"
	"github.com/gofiber/fiber/v2"
	"github.com/valyala/bytebufferpool"
)

func OEmbed() fiber.Handler {
	return func(c *fiber.Ctx) error {
		headingText := c.Query("text")
		headingURL := c.Query("url")
		if headingText == "" || headingURL == "" {
			return c.SendStatus(fiber.StatusBadRequest)
		}
		c.Set("Content-Type", "application/json")
		viewsBuf := bytebufferpool.Get()
		defer bytebufferpool.Put(viewsBuf)

		// Totally safe 100% valid template 👍
		OEmbedData := &model.OEmbedData{
			Text: escape.JSON(headingText),
			URL:  headingURL,
		}

		views.OEmbed(OEmbedData, viewsBuf)
		return c.Send(viewsBuf.Bytes())
	}
}
