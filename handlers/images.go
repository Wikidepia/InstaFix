package handlers

import (
	scraper "instafix/handlers/scraper"

	"github.com/gofiber/fiber/v2"
)

func Images() fiber.Handler {
	return func(c *fiber.Ctx) error {
		postID := c.Params("postID")
		mediaNum, err := c.ParamsInt("mediaNum", 1)
		if err != nil {
			return err
		}

		item, err := scraper.GetData(postID)
		if err != nil {
			return err
		}

		// Redirect to image URL
		if mediaNum > len(item.Medias) {
			return err
		}
		imageURL := item.Medias[max(1, mediaNum)-1].URL
		return c.Redirect(imageURL, fiber.StatusFound)
	}
}
