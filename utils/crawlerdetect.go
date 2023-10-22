package utils

import (
	"bytes"
)

var knownBots = [][]byte{
	[]byte("bot"),
	[]byte("facebook"),
	[]byte("embed"),
	[]byte("got"),
	[]byte("firefox/92"),
	[]byte("firefox/38"),
	[]byte("curl"),
	[]byte("wget"),
	[]byte("go-http"),
	[]byte("yahoo"),
	[]byte("generator"),
	[]byte("whatsapp"),
	[]byte("preview"),
	[]byte("link"),
	[]byte("proxy"),
	[]byte("vkshare"),
	[]byte("images"),
	[]byte("analyzer"),
	[]byte("index"),
	[]byte("crawl"),
	[]byte("spider"),
	[]byte("python"),
	[]byte("cfnetwork"),
	[]byte("node"),
	[]byte("mastodon"),
	[]byte("http.rb"),
}

func IsBot(userAgent []byte) bool {
	userAgent = bytes.ToLower(userAgent)
	for _, bot := range knownBots {
		if bytes.Contains(userAgent, bot) {
			return true
		}
	}
	return false
}
