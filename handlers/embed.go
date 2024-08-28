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

	"github.com/go-chi/chi/v5"
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

func Embed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	viewsData := &model.ViewsData{}

	var err error
	postID := chi.URLParam(r, "postID")
	mediaNumParams := chi.URLParam(r, "mediaNum")
	urlQuery := r.URL.Query()
	if urlQuery == nil {
		return
	}
	if mediaNumParams == "" {
		imgIndex := urlQuery.Get("img_index")
		if imgIndex != "" {
			mediaNumParams = imgIndex
		} else {
			mediaNumParams = "0"
		}
	}
	mediaNum, err := strconv.Atoi(mediaNumParams)
	if err != nil {
		viewsData.Description = "Invalid img_index parameter"
		views.Embed(viewsData, w)
		return
	}

	isDirect, _ := strconv.ParseBool(urlQuery.Get("direct"))
	isGallery, _ := strconv.ParseBool(urlQuery.Get("gallery"))

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
	viewsData.URL = "https://instagram.com" + strings.Replace(r.URL.RequestURI(), "/"+mediaNumParams, "", 1)
	if !utils.IsBot(r.Header.Get("User-Agent")) {
		http.Redirect(w, r, viewsData.URL, http.StatusFound)
		return
	}

	item, err := scraper.GetData(postID)
	if err != nil || len(item.Medias) == 0 {
		http.Redirect(w, r, viewsData.URL, http.StatusFound)
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

		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		viewsData.OEmbedURL = scheme + "://" + r.Host + "/oembed?text=" + url.QueryEscape(viewsData.Description) + "&url=" + viewsData.URL
	}
	if isDirect {
		http.Redirect(w, r, sb.String(), http.StatusFound)
		return
	}

	views.Embed(viewsData, w)
}
