package handlers

import (
	"instafix/views"
	"instafix/views/model"
	"net/http"

	"github.com/PurpleSec/escape"
)

func OEmbed(w http.ResponseWriter, r *http.Request) {
	urlQuery := r.URL.Query()
	if urlQuery == nil {
		return
	}
	headingText := urlQuery.Get("text")
	headingURL := urlQuery.Get("url")
	if headingText == "" || headingURL == "" {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	// Totally safe 100% valid template 👍
	OEmbedData := &model.OEmbedData{
		Text: escape.JSON(headingText),
		URL:  headingURL,
	}

	views.OEmbed(OEmbedData, w)
	return
}
