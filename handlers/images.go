package handlers

import (
	data "instafix/handlers/data"

	"github.com/gofiber/fiber/v2"
)

func Images() fiber.Handler {
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
		imageURL := item.Medias[max(1, mediaNum)-1].URL
		return c.Redirect(imageURL, fiber.StatusFound)
	}
}
