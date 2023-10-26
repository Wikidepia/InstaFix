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
			c.Response().SetBodyRaw(viewsBuf.Bytes())
			return nil
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
			c.Response().SetBodyRaw(viewsBuf.Bytes())
			return nil
		}

		if mediaNum > len(item.Medias) {
			viewsData.Description = "Media number out of range"
			views.Embed(viewsData, viewsBuf)
			c.Response().SetBodyRaw(viewsBuf.Bytes())
			return nil
		} else if len(item.Username) == 0 {
			viewsData.Description = "Post not found"
			views.Embed(viewsData, viewsBuf)
			c.Response().SetBodyRaw(viewsBuf.Bytes())
			return nil
		}

		var sb strings.Builder
		sb.Grow(32) // 32 bytes should be enough for most cases

		viewsData.Title = "@" + utils.B2S(item.Username)
		viewsData.Description = utils.B2S(item.Caption)

		typename := item.Medias[max(1, mediaNum)-1].TypeName
		isImage := bytes.Contains(typename, []byte("Image"))
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
			viewsData.OEmbedURL = c.BaseURL() + "/oembed?text=" + url.QueryEscape(viewsData.Description) + "&url=" + url.QueryEscape(viewsData.URL)
		}
		views.Embed(viewsData, viewsBuf)
		c.Response().SetBodyRaw(viewsBuf.Bytes())
		return nil
	}
}
