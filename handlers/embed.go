package handlers

import (
	"instafix/utils"
	"instafix/views"
	"strconv"
	"strings"

	data "instafix/handlers/data"

	"github.com/gofiber/fiber/v2"
)

func Embed() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/html; charset=utf-8")
		wr := c.Response().BodyWriter()
		viewsData := &views.ViewsData{}

		postID := c.Params("postID")
		mediaNum, err := c.ParamsInt("mediaNum", 0)
		if err != nil {
			viewsData.Description = "Invalid media number"
			views.Embed(viewsData, wr)
			return nil
		}

		// If User-Agent is not bot, redirect to Instagram
		viewsData.Title = "InstaFix"
		viewsData.URL = "https://instagram.com" + c.Path()
		if !utils.IsBot(c.Request().Header.UserAgent()) {
			return c.Redirect(viewsData.URL)
		}

		// Get data
		item := data.InstaData{PostID: postID}
		err = item.GetData(postID)
		if err != nil {
			viewsData.Description = "Post might not be available"
			views.Embed(viewsData, wr)
			return nil
		}

		if mediaNum > len(item.Medias) {
			viewsData.Description = "Media number out of range"
			views.Embed(viewsData, wr)
			return nil
		} else if len(item.Username) == 0 {
			viewsData.Description = "Post not found"
			views.Embed(viewsData, wr)
			return nil
		}

		var sb strings.Builder
		sb.Grow(32) // 32 bytes should be enough for most cases

		viewsData.Title = "@" + item.Username
		viewsData.Description = item.Caption

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
