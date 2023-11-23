package handlers

import (
	data "instafix/handlers/data"
	"instafix/utils"

	"github.com/gofiber/fiber/v2"
)

func Images() fiber.Handler {
	return func(c *fiber.Ctx) error {
		postID := c.Params("postID")
		mediaNum, err := c.ParamsInt("mediaNum", 1)
		if err != nil {
			return c.SendStatus(fiber.StatusNotFound)
		}

		// Get data
		item := &data.InstaData{}
		err = item.GetData(postID)
		if err != nil {
			return c.SendStatus(fiber.StatusNotFound)
		}

		// Redirect to image URL
		if mediaNum > len(item.Medias) {
			return c.SendStatus(fiber.StatusNotFound)
		}
		imageURL := item.Medias[max(1, mediaNum)-1].URL
		return c.Redirect(utils.B2S(imageURL), fiber.StatusFound)
	}
}
