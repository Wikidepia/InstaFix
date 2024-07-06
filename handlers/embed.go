package handlers

import (
	scraper "instafix/handlers/scraper"
	"instafix/utils"
	"instafix/views"
	"instafix/views/model"
	"net/url"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"
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
		viewsData := &model.ViewsData{}
		viewsBuf := bytebufferpool.Get()
		defer bytebufferpool.Put(viewsBuf)

		postID := c.Params("postID")
		mediaNum, err := c.ParamsInt("mediaNum", 0)
		if err != nil {
			viewsData.Description = "Invalid media number"
			views.Embed(viewsData, viewsBuf)
			return c.Send(viewsBuf.Bytes())
		}
		imgIndex := c.Query("img_index")
		if imgIndex != "" {
			mediaNum, err = strconv.Atoi(imgIndex)
			if err != nil {
				viewsData.Description = "Invalid img_index parameter"
				views.Embed(viewsData, viewsBuf)
				return c.Send(viewsBuf.Bytes())
			}
		}

		direct, err := strconv.ParseBool(c.Query("direct", "false"))
		if err != nil {
			viewsData.Description = "Invalid direct parameter"
			views.Embed(viewsData, viewsBuf)
			return c.Send(viewsBuf.Bytes())
		}

		isGallery, err := strconv.ParseBool(c.Query("gallery", "false"))
		if err != nil {
			viewsData.Description = "Invalid gallery parameter"
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

		item, err := scraper.GetData(postID)
		if err != nil || len(item.Medias) == 0 {
			viewsData.Description = "Post might not be available"
			views.Embed(viewsData, viewsBuf)
			return c.Send(viewsBuf.Bytes())
		}

		if mediaNum > len(item.Medias) {
			viewsData.Description = "Media number out of range"
			views.Embed(viewsData, viewsBuf)
			return c.Send(viewsBuf.Bytes())
		} else if len(item.Username) == 0 {
			log.Warn().Str("postID", postID).Msg("Post not found; empty username")
			viewsData.Description = "Post not found"
			views.Embed(viewsData, viewsBuf)
			return c.Send(viewsBuf.Bytes())
		}

		var sb strings.Builder
		sb.Grow(32) // 32 bytes should be enough for most cases

		viewsData.Title = "@" + item.Username
		// Gallery do not have any caption
		if !isGallery {
			viewsData.Description = item.Caption
			if len(viewsData.Description) > 255 {
				viewsData.Description = utils.Substr(viewsData.Description, 0, 250) + "..."
			}
		}

		typename := item.Medias[max(1, mediaNum)-1].TypeName
		isImage := strings.Contains(typename, "Image") || strings.Contains(typename, "StoryVideo")
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
			viewsData.OEmbedURL = c.BaseURL() + "/oembed?text=" + url.QueryEscape(viewsData.Description) + "&url=" + viewsData.URL
		}

		if direct {
			return c.Redirect(sb.String())
		}

		views.Embed(viewsData, viewsBuf)
		return c.Send(viewsBuf.Bytes())
	}
}
