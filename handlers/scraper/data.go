package handlers

import (
	"bytes"
	"errors"
	"instafix/utils"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/PurpleSec/escape"
	"github.com/cockroachdb/pebble"
	"github.com/kelindar/binary"
	"github.com/rs/zerolog/log"
	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/js"
	"github.com/tidwall/gjson"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttpproxy"
	"golang.org/x/net/html"
	"golang.org/x/sync/singleflight"
)

var gjsonNil = gjson.Result{}

var client = &fasthttp.Client{
	Dial:               fasthttpproxy.FasthttpProxyHTTPDialerTimeout(5 * time.Second),
	ReadBufferSize:     16 * 1024,
	MaxConnsPerHost:    1024,
	MaxConnWaitTimeout: 5 * time.Second,
}
var timeout = 10 * time.Second

var (
	ErrNotFound = errors.New("post not found")
)

var RemoteScraperAddr string

var sflightScraper singleflight.Group

type Media struct {
	TypeName string
	URL      string
}

type InstaData struct {
	PostID   string
	Username string
	Caption  string
	Medias   []Media
}

func GetData(postID string) (*InstaData, error) {
	cacheInstaData, closer, err := DB.Get(utils.S2B(postID))
	if err != nil && err != pebble.ErrNotFound {
		log.Error().Str("postID", postID).Err(err).Msg("Failed to get data from cache")
		return nil, err
	}

	if len(cacheInstaData) > 0 {
		i := &InstaData{PostID: postID}
		err := binary.Unmarshal(cacheInstaData, i)
		closer.Close()
		if err != nil {
			return nil, err
		}
		log.Debug().Str("postID", postID).Msg("Data parsed from cache")
		return i, nil
	}

	ret, err, _ := sflightScraper.Do(postID, func() (interface{}, error) {
		item := new(InstaData)
		item.PostID = postID
		if err := item.ScrapeData(); err != nil {
			if err != ErrNotFound {
				log.Error().Str("postID", item.PostID).Err(err).Msg("Failed to get data from Instagram")
			} else {
				log.Warn().Str("postID", item.PostID).Err(err).Msg("Post not found")
			}
			return item, err
		}

		// Replace all media urls cdn to scontent.cdninstagram.com
		for n, media := range item.Medias {
			u, err := url.Parse(media.URL)
			if err != nil {
				log.Error().Str("postID", item.PostID).Err(err).Msg("Failed to parse media URL")
				return false, err
			}
			u.Host = "scontent.cdninstagram.com"
			item.Medias[n].URL = u.String()
		}

		bb, err := binary.Marshal(item)
		if err != nil {
			log.Error().Str("postID", item.PostID).Err(err).Msg("Failed to marshal data")
			return false, err
		}

		batch := DB.NewBatch()
		// Write cache to DB
		if err := batch.Set(utils.S2B(item.PostID), bb, pebble.Sync); err != nil {
			log.Error().Str("postID", item.PostID).Err(err).Msg("Failed to save data to cache")
			return false, err
		}

		// Write expire to DB
		expTime := strconv.FormatInt(time.Now().Add(24*time.Hour).UnixNano(), 10)
		if err := batch.Set(append([]byte("exp-"), expTime...), utils.S2B(item.PostID), pebble.Sync); err != nil {
			log.Error().Str("postID", item.PostID).Err(err).Msg("Failed to save data to cache")
			return false, err
		}

		// Commit batch
		if err := batch.Commit(pebble.Sync); err != nil {
			log.Error().Str("postID", item.PostID).Err(err).Msg("Failed to commit batch")
			return false, err
		}
		return item, nil
	})
	if err != nil {
		return nil, err
	}
	return ret.(*InstaData), nil
}

func (i *InstaData) ScrapeData() error {
	var gqlData gjson.Result

	req, res := fasthttp.AcquireRequest(), fasthttp.AcquireResponse()
	defer func() {
		fasthttp.ReleaseRequest(req)
		fasthttp.ReleaseResponse(res)
	}()

	// Scrape from remote scraper if available
	if len(RemoteScraperAddr) > 0 {
		req.Header.SetMethod("GET")
		req.Header.Set("Accept-Encoding", "gzip, deflate, br")
		req.SetRequestURI(RemoteScraperAddr + "/scrape/" + i.PostID)
		if err := client.DoTimeout(req, res, timeout); err == nil && res.StatusCode() == fasthttp.StatusOK {
			iDataGunzip, _ := res.BodyGunzip()
			if err := binary.Unmarshal(iDataGunzip, i); err == nil {
				log.Info().Str("postID", i.PostID).Msg("Data parsed from remote scraper")
				return nil
			}
		}
	}

	req.Reset()
	res.Reset()

	// Embed scraper
	req.Header.SetMethod("GET")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Connection", "close")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/118.0.0.0 Safari/537.36")
	req.SetRequestURI("https://www.instagram.com/p/" + i.PostID + "/embed/captioned/")

	var err error
	for retries := 0; retries < 3; retries++ {
		err := client.DoTimeout(req, res, timeout)
		if err == nil && len(res.Body()) > 0 {
			break
		}
	}

	// Pattern matching using LDE
	l := &Line{}

	// TimeSliceImpl
	ldeMatch := false
	for _, line := range bytes.Split(res.Body(), []byte("\n")) {
		// Check if line contains TimeSliceImpl
		ldeMatch, _ = l.Extract(line)
	}

	if ldeMatch {
		lexer := js.NewLexer(parse.NewInputBytes(l.GetTimeSliceImplValue()))
		for {
			tt, text := lexer.Next()
			if tt == js.ErrorToken {
				break
			}
			if tt == js.StringToken && bytes.Contains(text, []byte("shortcode_media")) {
				// Strip quotes from start and end
				text = text[1 : len(text)-1]
				unescapeData := utils.UnescapeJSONString(utils.B2S(text))
				if !gjson.Valid(unescapeData) {
					log.Error().Str("postID", i.PostID).Err(err).Msg("Failed to parse data from TimeSliceImpl")
					return err
				}
				timeSlice := gjson.Parse(unescapeData)
				log.Info().Str("postID", i.PostID).Msg("Data parsed from TimeSliceImpl")
				gqlData = timeSlice.Get("gql_data")
			}
		}
	}

	// Scrape from embed HTML
	embedHTML, err := scrapeFromEmbedHTML(res.Body())
	if err != nil {
		log.Error().Str("postID", i.PostID).Err(err).Msg("Failed to parse data from scrapeFromEmbedHTML")
		return err
	}

	embedHTMLData := gjson.Parse(embedHTML)

	smedia := embedHTMLData.Get("shortcode_media")
	videoBlocked := smedia.Get("video_blocked").Bool()
	username := smedia.Get("owner.username").String()

	// Scrape from GraphQL API
	if videoBlocked || len(username) == 0 {
		gqlValue, err := scrapeFromGQL(i.PostID, req, res)
		if err != nil {
			log.Error().Str("postID", i.PostID).Err(err).Msg("Failed to scrape data from scrapeFromGQL")
			return err
		}
		gqlData := gjson.Parse(utils.B2S(gqlValue))
		if gqlData.Get("data").Exists() {
			log.Info().Str("postID", i.PostID).Msg("Data scraped from scrapeFromGQL")
			gqlData = gqlData.Get("data")
		}
	}

	item := gqlData.Get("shortcode_media")
	if !item.Exists() {
		item = gqlData.Get("xdt_shortcode_media")
		if !item.Exists() {
			log.Error().Str("postID", i.PostID).Msg("Failed to parse data from Instagram")
			return ErrNotFound
		}
	}

	media := []gjson.Result{item}
	if item.Get("edge_sidecar_to_children").Exists() {
		media = item.Get("edge_sidecar_to_children.edges").Array()
	}

	// Get username
	i.Username = item.Get("owner.username").String()

	// Get caption
	i.Caption = strings.TrimSpace(item.Get("edge_media_to_caption.edges.0.node.text").String())

	// Get medias
	i.Medias = make([]Media, 0, len(media))
	for _, m := range media {
		if m.Get("node").Exists() {
			m = m.Get("node")
		}
		mediaURL := m.Get("video_url")
		if !mediaURL.Exists() {
			mediaURL = m.Get("display_url")
		}
		i.Medias = append(i.Medias, Media{
			TypeName: m.Get("__typename").String(),
			URL:      mediaURL.String(),
		})
	}

	// Failed to scrape from Embed
	if len(username) == 0 {
		return ErrNotFound
	}
	return nil
}

// Taken from https://github.com/PuerkitoBio/goquery
// Modified to add new line every <br>
func gqTextNewLine(s *goquery.Selection) string {
	// Slightly optimized vs calling Each: no single selection object created
	var sb strings.Builder
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.TextNode {
			// Keep newlines and spaces, like jQuery
			sb.WriteString(n.Data)
		} else if n.Type == html.ElementNode && n.Data == "br" {
			sb.WriteString("\n")
		}
		if n.FirstChild != nil {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				f(c)
			}
		}
	}
	for _, n := range s.Nodes {
		f(n)
	}
	return sb.String()
}

func scrapeFromEmbedHTML(embedHTML []byte) (string, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(embedHTML))
	if err != nil {
		return "", err
	}

	// Get media URL
	typename := "GraphImage"
	embedMedia := doc.Find(".EmbeddedMediaImage")
	if embedMedia.Length() == 0 {
		typename = "GraphVideo"
		embedMedia = doc.Find(".EmbeddedMediaVideo")
	}
	mediaURL, _ := embedMedia.Attr("src")

	// Get username
	username := doc.Find(".UsernameText").Text()

	// Get caption
	captionComments := doc.Find(".CaptionComments")
	if captionComments.Length() > 0 {
		captionComments.Remove()
	}
	captionUsername := doc.Find(".CaptionUsername")
	if captionUsername.Length() > 0 {
		captionUsername.Remove()
	}
	caption := gqTextNewLine(doc.Find(".Caption"))

	// Check if contains WatchOnInstagram
	videoBlocked := strconv.FormatBool(bytes.Contains(embedHTML, []byte("WatchOnInstagram")))

	// Totally safe 100% valid JSON 👍
	return `{
		"shortcode_media": {
			"owner": {"username": "` + username + `"},
			"node": {"__typename": "` + typename + `", "display_url": "` + mediaURL + `"},
			"edge_media_to_caption": {"edges": [{"node": {"text": ` + escape.JSON(caption) + `}}]},
			"dimensions": {"height": null, "width": null},
			"video_blocked": ` + videoBlocked + `
		}
	}`, nil
}

func scrapeFromGQL(postID string, req *fasthttp.Request, res *fasthttp.Response) ([]byte, error) {
	req.Reset()
	res.Reset()

	req.Header.SetMethod("POST")
	req.Header.Set("accept", "*/*")
	req.Header.Set("accept-language", "en-US,en;q=0.9")
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("origin", "https://www.instagram.com")
	req.Header.Set("priority", "u=1, i")
	req.Header.Set("sec-ch-prefers-color-scheme", "dark")
	req.Header.Set("sec-ch-ua", `"Google Chrome";v="125", "Chromium";v="125", "Not.A/Brand";v="24"`)
	req.Header.Set("sec-ch-ua-full-version-list", `"Google Chrome";v="125.0.6422.142", "Chromium";v="125.0.6422.142", "Not.A/Brand";v="24.0.0.0"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-model", `""`)
	req.Header.Set("sec-ch-ua-platform", `"macOS"`)
	req.Header.Set("sec-ch-ua-platform-version", `"12.7.4"`)
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36")
	req.Header.Set("x-asbd-id", "129477")
	req.Header.Set("x-bloks-version-id", "e2004666934296f275a5c6b2c9477b63c80977c7cc0fd4b9867cb37e36092b68")
	req.Header.Set("x-fb-friendly-name", "PolarisPostActionLoadPostQueryQuery")
	req.Header.Set("x-ig-app-id", "936619743392459")

	req.SetRequestURI("https://www.instagram.com/graphql/query/")
	gqlParams := url.Values{
		"av":                       {"0"},
		"__d":                      {"www"},
		"__user":                   {"0"},
		"__a":                      {"1"},
		"__req":                    {"k"},
		"__hs":                     {"19888.HYP:instagram_web_pkg.2.1..0.0"},
		"dpr":                      {"2"},
		"__ccg":                    {"UNKNOWN"},
		"__rev":                    {"1014227545"},
		"__s":                      {"trbjos:n8dn55:yev1rm"},
		"__hsi":                    {"7380500578385702299"},
		"__dyn":                    {"7xeUjG1mxu1syUbFp40NonwgU7SbzEdF8aUco2qwJw5ux609vCwjE1xoswaq0yE6ucw5Mx62G5UswoEcE7O2l0Fwqo31w9a9wtUd8-U2zxe2GewGw9a362W2K0zK5o4q3y1Sx-0iS2Sq2-azo7u3C2u2J0bS1LwTwKG1pg2fwxyo6O1FwlEcUed6goK2O4UrAwCAxW6Uf9EObzVU8U"},
		"__csr":                    {"n2Yfg_5hcQAG5mPtfEzil8Wn-DpKGBXhdczlAhrK8uHBAGuKCJeCieLDyExenh68aQAKta8p8ShogKkF5yaUBqCpF9XHmmhoBXyBKbQp0HCwDjqoOepV8Tzk8xeXqAGFTVoCciGaCgvGUtVU-u5Vp801nrEkO0rC58xw41g0VW07ISyie2W1v7F0CwYwwwvEkw8K5cM0VC1dwdi0hCbc094w6MU1xE02lzw"},
		"__comet_req":              {"7"},
		"lsd":                      {"AVoPBTXMX0Y"},
		"jazoest":                  {"2882"},
		"__spin_r":                 {"1014227545"},
		"__spin_b":                 {"trunk"},
		"__spin_t":                 {"1718406700"},
		"fb_api_caller_class":      {"RelayModern"},
		"fb_api_req_friendly_name": {"PolarisPostActionLoadPostQueryQuery"},
		"variables":                {`{"shortcode":"` + postID + `","fetch_comment_count":40,"parent_comment_count":24,"child_comment_count":3,"fetch_like_count":10,"fetch_tagged_user_count":null,"fetch_preview_comment_count":2,"has_threaded_comments":true,"hoisted_comment_id":null,"hoisted_reply_id":null}`},
		"server_timestamps":        {"true"},
		"doc_id":                   {"25531498899829322"},
	}
	req.SetBodyString(gqlParams.Encode())

	if err := client.DoTimeout(req, res, timeout); err != nil {
		return nil, err
	}
	return res.Body(), nil
}
