package handlers

import (
	"instafix/utils"
	"instafix/views"
	"instafix/views/model"
	"net/http"
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
		Text: utils.EscapeJSONString(headingText),
		URL:  headingURL,
	}

	views.OEmbed(OEmbedData, w)
}
