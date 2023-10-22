package handlers

import (
	data "instafix/handlers/data"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func Videos() fiber.Handler {
	return func(c *fiber.Ctx) error {
		postID := c.Params("postID")
		mediaNum, err := c.ParamsInt("mediaNum", 1)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		// Get data
		item := &data.InstaData{PostID: postID}
		err = item.GetData(postID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		// Redirect to image URL
		if mediaNum > len(item.Medias) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "Media not found",
			})
		}
		videoURL := item.Medias[max(1, mediaNum)-1].URL

		// Redirect to proxy if not TelegramBot in User-Agent
		if strings.Contains(c.Get("User-Agent"), "TelegramBot") {
			return c.Redirect(videoURL, fiber.StatusFound)
		}
		return c.Redirect("https://envoy.lol/"+videoURL, fiber.StatusFound)
	}
}
