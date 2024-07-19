package utils

import (
	"strings"
)

var knownBots = []string{
	"bot",
	"facebook",
	"embed",
	"got",
	"firefox/92",
	"firefox/38",
	"curl",
	"wget",
	"go-http",
	"yahoo",
	"generator",
	"whatsapp",
	"preview",
	"link",
	"proxy",
	"vkshare",
	"images",
	"analyzer",
	"index",
	"crawl",
	"spider",
	"python",
	"cfnetwork",
	"node",
	"mastodon",
	"http.rb",
	"discord",
	"ruby",
	"bun/",
	"fiddler",
	"revoltchat",
}

func IsBot(userAgent string) bool {
	userAgent = strings.ToLower(userAgent)
	for _, bot := range knownBots {
		if strings.Contains(userAgent, bot) {
			return true
		}
	}
	return false
}
