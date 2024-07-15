package handlers

import (
	scraper "instafix/handlers/scraper"
	"net/http"
	"strconv"
)

func Images(w http.ResponseWriter, r *http.Request) {
	postID := r.PathValue("postID")
	mediaNum, err := strconv.Atoi(r.PathValue("mediaNum"))
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
	imageURL := item.Medias[max(1, mediaNum)-1].URL
	w.Header().Set("Location", imageURL)
	return
}
