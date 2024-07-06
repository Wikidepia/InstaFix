package handlers

import (
	scraper "instafix/handlers/scraper"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func Videos() fiber.Handler {
	return func(c *fiber.Ctx) error {
		postID := c.Params("postID")
		mediaNum, err := c.ParamsInt("mediaNum", 1)
		if err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}

		// Get data
		var item scraper.InstaData
		err = item.GetData(postID)
		if err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}

		// Redirect to image URL
		if mediaNum > len(item.Medias) {
			return c.SendStatus(fiber.StatusNotFound)
		}
		videoURL := item.Medias[max(1, mediaNum)-1].URL

		// Redirect to proxy if not TelegramBot in User-Agent
		if strings.Contains(c.Get("User-Agent"), "TelegramBot") {
			return c.Redirect(videoURL, fiber.StatusFound)
		}
		return c.Redirect("https://envoy.lol/"+videoURL, fiber.StatusFound)
	}
}
