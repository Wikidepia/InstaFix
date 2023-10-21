package handlers

import (
	"instafix/views"
	"strconv"
	"strings"

	data "instafix/handlers/data"

	"github.com/gofiber/fiber/v2"
)

func Embed() fiber.Handler {
	return func(c *fiber.Ctx) error {
		postID := c.Params("postID")
		mediaNum, err := c.ParamsInt("mediaNum", 0)
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

		// Return embed template
		c.Set("Content-Type", "text/html")

		wr := c.Response().BodyWriter()

		viewsData := &views.ViewsData{
			Title:       "@" + item.Username,
			URL:         "https://instagram.com/p/" + postID,
			Description: item.Caption,
		}

		var sb strings.Builder
		sb.Grow(32) // 32 bytes should be enough for most cases

		typename := item.Medias[max(1, mediaNum)-1].TypeName
		isImage := strings.Contains(typename, "Image")
		if mediaNum == 0 && isImage {
			viewsData.Card = "summary_large_image"
			sb.WriteString("/grid/")
			sb.WriteString(postID)
			viewsData.ImageURL = sb.String()
		} else if isImage {
			viewsData.Card = "summary_large_image"
			sb.WriteString("/images/")
			sb.WriteString(postID)
			sb.WriteString("/")
			sb.WriteString(strconv.Itoa(max(1, mediaNum)))
			viewsData.ImageURL = sb.String()
		} else {
			viewsData.Card = "player"
			sb.WriteString("/videos/")
			sb.WriteString(postID)
			sb.WriteString("/")
			sb.WriteString(strconv.Itoa(max(1, mediaNum)))
			viewsData.VideoURL = sb.String()
		}

		views.Embed(viewsData, wr)
		return nil
	}
}
