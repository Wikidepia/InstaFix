package handlers

import (
	scraper "instafix/handlers/scraper"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

func Images(w http.ResponseWriter, r *http.Request) {
	postID := chi.URLParam(r, "postID")
	mediaNum, err := strconv.Atoi(chi.URLParam(r, "mediaNum"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	item, err := scraper.GetData(postID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Redirect to image URL
	if mediaNum > len(item.Medias) {
		return
	}
	imageURL := item.Medias[max(1, mediaNum)-1].URL
	http.Redirect(w, r, imageURL, http.StatusFound)
}
