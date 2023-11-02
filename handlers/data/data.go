package handlers

import (
	"bytes"
	"errors"
	"instafix/utils"
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
	"github.com/valyala/bytebufferpool"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttpproxy"
	"github.com/valyala/fastjson"
	"golang.org/x/net/html"
)

var client = &fasthttp.Client{Dial: fasthttpproxy.FasthttpProxyHTTPDialer(), ReadBufferSize: 16 * 1024}
var timeout = 10 * time.Second
var parserPool fastjson.ParserPool

var (
	ErrNotFound = errors.New("post not found")
)

type Media struct {
	TypeName []byte
	URL      []byte
}

type InstaData struct {
	PostID   []byte
	Username []byte
	Caption  []byte
	Medias   []Media
	Expire   uint32
}

func (i *InstaData) GetData(postID string) error {
	cacheInstaData, closer, err := DB.Get(utils.S2B(postID))
	if err != nil && err != pebble.ErrNotFound {
		log.Error().Str("postID", postID).Err(err).Msg("Failed to get data from cache")
		return err
	}

	if len(cacheInstaData) > 0 {
		err := binary.Unmarshal(cacheInstaData, i)
		closer.Close()
		if err != nil {
			return err
		}
		// Check if expired, if not return data from cache
		if i.Expire > uint32(time.Now().Unix()) {
			log.Info().Str("postID", postID).Msg("Data parsed from cache")
			return nil
		}
	}

	// Get data from Instagram
	p := parserPool.Get()
	defer parserPool.Put(p)
	data, err := getData(postID, p)
	if err != nil {
		if err != ErrNotFound {
			log.Error().Str("postID", postID).Err(err).Msg("Failed to get data from Instagram")
		}
		return err
	}

	if data == (*fastjson.Value)(nil) {
		return errors.New("data is nil")
	}

	item := data.Get("shortcode_media")
	if item == nil {
		return errors.New("shortcode_media not found")
	}

	media := []*fastjson.Value{item}
	if item.Exists("edge_sidecar_to_children") {
		media = item.GetArray("edge_sidecar_to_children", "edges")
	}

	// Get username
	i.Username = item.GetStringBytes("owner", "username")

	// Get caption
	i.Caption = bytes.TrimSpace(item.GetStringBytes("edge_media_to_caption", "edges", "0", "node", "text"))

	// Get medias
	i.Medias = make([]Media, 0, len(media))
	for _, m := range media {
		if m.Exists("node") {
			m = m.Get("node")
		}
		mediaURL := m.GetStringBytes("video_url")
		if mediaURL == nil {
			mediaURL = m.GetStringBytes("display_url")
		}
		i.Medias = append(i.Medias, Media{
			TypeName: m.GetStringBytes("__typename"),
			URL:      mediaURL,
		})
	}

	// Set expire
	i.Expire = uint32(time.Now().Add(24 * time.Hour).Unix())

	bb := bytebufferpool.Get()
	defer bytebufferpool.Put(bb)
	err = binary.MarshalTo(i, bb)
	if err := DB.Set(utils.S2B(postID), bb.Bytes(), pebble.Sync); err != nil {
		log.Error().Str("postID", postID).Err(err).Msg("Failed to save data to cache")
		return err
	}
	return nil
}

func getData(postID string, p *fastjson.Parser) (*fastjson.Value, error) {
	req, res := fasthttp.AcquireRequest(), fasthttp.AcquireResponse()
	defer func() {
		fasthttp.ReleaseRequest(req)
		fasthttp.ReleaseResponse(res)
	}()

	req.Header.SetMethod("GET")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Connection", "close")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/118.0.0.0 Safari/537.36")
	req.SetRequestURI("https://www.instagram.com/p/" + postID + "/embed/captioned/")

	var err error
	for retries := 0; retries < 3; retries++ {
		err := client.DoTimeout(req, res, timeout)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, err
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
				timeSlice, err := p.Parse(unescapeData)
				if err != nil {
					log.Error().Str("postID", postID).Err(err).Msg("Failed to parse data from TimeSliceImpl")
					return nil, err
				}
				log.Info().Str("postID", postID).Msg("Data parsed from TimeSliceImpl")
				return timeSlice.Get("gql_data"), nil
			}
		}
	}

	// Parse embed HTML
	embedHTML, err := ParseEmbedHTML(res.Body())
	if err != nil {
		log.Error().Str("postID", postID).Err(err).Msg("Failed to parse data from ParseEmbedHTML")
		return nil, err
	}
	embedHTMLData, err := p.ParseBytes(embedHTML)
	if err != nil {
		return nil, err
	}

	smedia := embedHTMLData.Get("shortcode_media")
	videoBlocked := smedia.GetBool("video_blocked")
	username := smedia.GetStringBytes("owner", "username")

	// Scrape from GraphQL API
	if videoBlocked || len(username) == 0 {
		gqlValue, err := parseGQLData(postID)
		if err != nil {
			log.Error().Str("postID", postID).Err(err).Msg("Failed to parse data from parseGQLData")
			return nil, err
		}
		gqlData, err := p.ParseBytes(gqlValue)
		if err != nil {
			return nil, err
		}
		if gqlData.Exists("data") {
			log.Info().Str("postID", postID).Msg("Data parsed from parseGQLData")
			return gqlData.Get("data"), nil
		}
	}

	// Check if contains "ebmMessage" (error message)
	if bytes.Contains(res.Body(), []byte("ebmMessage")) {
		return nil, ErrNotFound
	}

	log.Info().Str("postID", postID).Msg("Data parsed from ParseEmbedHTML")
	return embedHTMLData, nil
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

func ParseEmbedHTML(embedHTML []byte) ([]byte, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(embedHTML))
	if err != nil {
		return nil, err
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
	return utils.S2B(`{
		"shortcode_media": {
			"owner": {"username": "` + username + `"},
			"node": {"__typename": "` + typename + `", "display_url": "` + mediaURL + `"},
			"edge_media_to_caption": {"edges": [{"node": {"text": ` + escape.JSON(caption) + `}}]},
			"dimensions": {"height": null, "width": null},
			"video_blocked": ` + videoBlocked + `
		}
	}`), nil
}

func parseGQLData(postID string) ([]byte, error) {
	req, res := fasthttp.AcquireRequest(), fasthttp.AcquireResponse()
	defer func() {
		fasthttp.ReleaseRequest(req)
		fasthttp.ReleaseResponse(res)
	}()

	req.Header.SetMethod("GET")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Connection", "close")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/118.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://www.instagram.com/p/"+postID+"/")

	req.SetRequestURI("https://www.instagram.com/graphql/query/")
	req.URI().QueryArgs().Add("query_hash", "b3055c01b4b222b8a47dc12b090e4e64")
	req.URI().QueryArgs().Add("variables", "{\"shortcode\":\""+postID+"\"}")

	if err := client.Do(req, res); err != nil {
		return nil, err
	}
	return res.Body(), nil
}
