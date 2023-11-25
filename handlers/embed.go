package handlers

import (
	"bytes"
	"instafix/utils"
	"instafix/views"
	"net/url"
	"strconv"
	"strings"

	data "instafix/handlers/data"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/bytebufferpool"
)

func mediaidToCode(mediaID int) string {
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	var shortCode string

	for mediaID > 0 {
		remainder := mediaID % 64
		mediaID /= 64
		shortCode = string(alphabet[remainder]) + shortCode
	}

	return shortCode
}

func Embed() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/html; charset=utf-8")
		viewsData := &views.ViewsData{}
		viewsBuf := bytebufferpool.Get()
		defer bytebufferpool.Put(viewsBuf)

		postID := c.Params("postID")
		mediaNum, err := c.ParamsInt("mediaNum", 0)
		if err != nil {
			viewsData.Description = "Invalid media number"
			views.Embed(viewsData, viewsBuf)
			return c.Send(viewsBuf.Bytes())
		}
		direct, err := strconv.ParseBool(c.Query("direct", "false"))
		if err != nil {
			viewsData.Description = "Invalid direct parameter"
			views.Embed(viewsData, viewsBuf)
			return c.Send(viewsBuf.Bytes())
		}

		// Stories use mediaID (int) instead of postID
		if strings.Contains(c.Path(), "/stories/") {
			mediaID, err := strconv.Atoi(postID)
			if err != nil {
				viewsData.Description = "Invalid postID"
				views.Embed(viewsData, viewsBuf)
				c.Send(viewsBuf.Bytes())
				return nil
			}
			postID = mediaidToCode(mediaID)
		}

		// If User-Agent is not bot, redirect to Instagram
		viewsData.Title = "InstaFix"
		viewsData.URL = "https://instagram.com" + c.Path()
		if !utils.IsBot(c.Request().Header.UserAgent()) {
			return c.Redirect(viewsData.URL)
		}

		// Get data
		item := data.InstaData{}
		err = item.GetData(postID)
		if err != nil {
			viewsData.Description = "Post might not be available"
			views.Embed(viewsData, viewsBuf)
			return c.Send(viewsBuf.Bytes())
		}

		if mediaNum > len(item.Medias) {
			viewsData.Description = "Media number out of range"
			views.Embed(viewsData, viewsBuf)
			return c.Send(viewsBuf.Bytes())
		} else if len(item.Username) == 0 {
			viewsData.Description = "Post not found"
			views.Embed(viewsData, viewsBuf)
			return c.Send(viewsBuf.Bytes())
		}

		var sb strings.Builder
		sb.Grow(32) // 32 bytes should be enough for most cases

		viewsData.Title = "@" + utils.B2S(item.Username)
		viewsData.Description = utils.B2S(item.Caption)

		typename := item.Medias[max(1, mediaNum)-1].TypeName
		isImage := bytes.Contains(typename, []byte("Image")) || bytes.Contains(typename, []byte("StoryVideo"))
		switch {
		case mediaNum == 0 && isImage && len(item.Medias) > 1:
			viewsData.Card = "summary_large_image"
			sb.WriteString("/grid/")
			sb.WriteString(postID)
			viewsData.ImageURL = sb.String()
		case isImage:
			viewsData.Card = "summary_large_image"
			sb.WriteString("/images/")
			sb.WriteString(postID)
			sb.WriteString("/")
			sb.WriteString(strconv.Itoa(max(1, mediaNum)))
			viewsData.ImageURL = sb.String()
		default:
			viewsData.Card = "player"
			sb.WriteString("/videos/")
			sb.WriteString(postID)
			sb.WriteString("/")
			sb.WriteString(strconv.Itoa(max(1, mediaNum)))
			viewsData.VideoURL = sb.String()
			viewsData.OEmbedURL = c.BaseURL() + "/oembed?text=" + url.QueryEscape(viewsData.Description) + "&url=" + url.QueryEscape(viewsData.URL)
		}

		if direct {
			return c.Redirect(sb.String())
		}

		views.Embed(viewsData, viewsBuf)
		return c.Send(viewsBuf.Bytes())
	}
}
