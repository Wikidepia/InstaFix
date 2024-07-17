package handlers

import (
	scraper "instafix/handlers/scraper"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

func Videos(w http.ResponseWriter, r *http.Request) {
	postID := chi.URLParam(r, "postID")
	mediaNum, err := strconv.Atoi(chi.URLParam(r, "mediaNum"))
	if err != nil {
		return
	}

	item, err := scraper.GetData(postID)
	if err != nil {
		return
	}

	// Redirect to image URL
	if mediaNum > len(item.Medias) {
		return
	}
	videoURL := item.Medias[max(1, mediaNum)-1].URL

	// Redirect to proxy if not TelegramBot in User-Agent
	if strings.Contains(r.Header.Get("User-Agent"), "TelegramBot") {
		http.Redirect(w, r, videoURL, http.StatusFound)
		return
	}
	http.Redirect(w, r, "https://envoy.lol/"+videoURL, http.StatusFound)
	return
}
