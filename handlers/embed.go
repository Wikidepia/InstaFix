package handlers

import (
	scraper "instafix/handlers/scraper"
	"instafix/utils"
	"instafix/views"
	"instafix/views/model"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/julienschmidt/httprouter"
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

func Embed(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	viewsData := &model.ViewsData{}
	viewsBuf := bytebufferpool.Get()
	defer bytebufferpool.Put(viewsBuf)

	var err error
	var mediaNum int
	urlQuery := r.URL.Query()
	postID := ps.ByName("postID")
	mediaNumParams := ps.ByName("mediaNum")
	if mediaNumParams == "" {
		imgIndex := urlQuery.Get("img_index")
		if imgIndex != "" {
			mediaNumParams = imgIndex
		}
		mediaNum, err = strconv.Atoi(mediaNumParams)
		if err != nil {
			viewsData.Description = "Invalid img_index parameter"
			views.Embed(viewsData, w)
			return
		}
	}

	direct, err := strconv.ParseBool(urlQuery.Get("direct"))
	if err != nil {
		viewsData.Description = "Invalid direct parameter"
		views.Embed(viewsData, w)
		return
	}

	isGallery, err := strconv.ParseBool(urlQuery.Get("gallery"))
	if err != nil {
		viewsData.Description = "Invalid gallery parameter"
		views.Embed(viewsData, w)
		return
	}

	// Stories use mediaID (int) instead of postID
	if strings.Contains(r.URL.Path, "/stories/") {
		mediaID, err := strconv.Atoi(postID)
		if err != nil {
			viewsData.Description = "Invalid postID"
			views.Embed(viewsData, w)
			return
		}
		postID = mediaidToCode(mediaID)
	}

	// If User-Agent is not bot, redirect to Instagram
	viewsData.Title = "InstaFix"
	viewsData.URL = "https://instagram.com" + r.URL.Path
	if !utils.IsBot(r.Header.Get("User-Agent")) {
		w.Header().Set("Location", viewsData.URL)
		return
	}

	item, err := scraper.GetData(postID)
	if err != nil || len(item.Medias) == 0 {
		viewsData.Description = "Post might not be available"
		views.Embed(viewsData, w)
		return
	}

	if mediaNum > len(item.Medias) {
		viewsData.Description = "Media number out of range"
		views.Embed(viewsData, w)
		return
	} else if len(item.Username) == 0 {
		viewsData.Description = "Post not found"
		views.Embed(viewsData, w)
		return
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
		viewsData.OEmbedURL = r.Host + "/oembed?text=" + url.QueryEscape(viewsData.Description) + "&url=" + viewsData.URL
	}

	if direct {
		w.Header().Set("Location", sb.String())
		return
	}

	views.Embed(viewsData, w)
	return
}
