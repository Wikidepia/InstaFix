package handlers

import (
	scraper "instafix/handlers/scraper"
	"net/http"
	"strconv"
	"strings"

	"github.com/julienschmidt/httprouter"
)

func Videos(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	postID := ps.ByName("postID")
	mediaNum, err := strconv.Atoi(ps.ByName("mediaNum"))
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
		w.Header().Set("Location", videoURL)
		return
	}
	w.Header().Set("Location", "https://envoy.lol/"+videoURL)
	return
}
