package handlers

import (
	"instafix/views"

	"github.com/PurpleSec/escape"
	"github.com/gofiber/fiber/v2"
)

func OEmbed() fiber.Handler {
	return func(c *fiber.Ctx) error {
		headingText := c.Query("text")
		headingURL := c.Query("url")
		if headingText == "" || headingURL == "" {
			return c.SendStatus(fiber.StatusBadRequest)
		}

		c.Set("Content-Type", "application/json")
		wr := c.Response().BodyWriter()

		// Totally safe 100% valid template 👍
		OEmbedData := &views.OEmbedData{
			Text: escape.JSON(headingText),
			URL:  headingURL,
		}
		views.OEmbed(OEmbedData, wr)
		return nil
	}
}
