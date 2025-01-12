package handlers

import (
	"bytes"
	_ "embed"
	"errors"
	"instafix/utils"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/kelindar/binary"
	"github.com/klauspost/compress/gzhttp"
	"github.com/klauspost/compress/zstd"
	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/js"
	"github.com/tidwall/gjson"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/net/html"
	"golang.org/x/sync/singleflight"
)

var (
	RemoteScraperAddr string
	ErrNotFound       = errors.New("post not found")
	timeout           = 5 * time.Second
	transport         http.RoundTripper
	transportNoProxy  *http.Transport
	sflightScraper    singleflight.Group
	remoteZSTDReader  *zstd.Decoder
)

//go:embed dictionary.bin
var zstdDict []byte

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

func init() {
	var err error
	transport = gzhttp.Transport(http.DefaultTransport, gzhttp.TransportAlwaysDecompress(true))
	transportNoProxy = http.DefaultTransport.(*http.Transport).Clone()
	transportNoProxy.Proxy = nil // Skip any proxy

	remoteZSTDReader, err = zstd.NewReader(nil, zstd.WithDecoderLowmem(true), zstd.WithDecoderDicts(zstdDict))
	if err != nil {
		panic(err)
	}
}

func GetData(postID string) (*InstaData, error) {
	if len(postID) == 0 || (postID[0] != 'C' && postID[0] != 'D' && postID[0] != 'B') {
		return nil, errors.New("postID is not a valid Instagram post ID")
	}

	i := &InstaData{PostID: postID}
	err := DB.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("data"))
		if b == nil {
			return nil
		}
		v := b.Get([]byte(postID))
		if v == nil {
			return nil
		}
		err := binary.Unmarshal(v, i)
		if err != nil {
			return err
		}
		slog.Debug("Data parsed from cache", "postID", postID)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Successfully parsed from cache
	if len(i.Medias) != 0 {
		return i, nil
	}

	ret, err, _ := sflightScraper.Do(postID, func() (interface{}, error) {
		item := new(InstaData)
		item.PostID = postID
		if err := item.ScrapeData(); err != nil {
			slog.Error("Failed to scrape data from Instagram", "postID", item.PostID, "err", err)
			return nil, err
		}

		// Replace all media urls cdn to scontent.cdninstagram.com
		for n, media := range item.Medias {
			u, err := url.Parse(media.URL)
			if err != nil {
				slog.Error("Failed to parse media URL", "postID", item.PostID, "err", err)
				return false, err
			}
			u.Host = "scontent.cdninstagram.com"
			item.Medias[n].URL = u.String()
		}

		bb, err := binary.Marshal(item)
		if err != nil {
			slog.Error("Failed to marshal data", "postID", item.PostID, "err", err)
			return false, err
		}

		err = DB.Batch(func(tx *bolt.Tx) error {
			dataBucket := tx.Bucket([]byte("data"))
			if dataBucket == nil {
				return nil
			}
			dataBucket.Put(utils.S2B(item.PostID), bb)

			ttlBucket := tx.Bucket([]byte("ttl"))
			if ttlBucket == nil {
				return nil
			}
			expTime := strconv.FormatInt(time.Now().Add(24*time.Hour).UnixNano(), 10)
			ttlBucket.Put(utils.S2B(expTime), utils.S2B(item.PostID))
			return nil
		})
		if err != nil {
			slog.Error("Failed to save data to cache", "postID", item.PostID, "err", err)
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
	// Scrape from remote scraper if available
	if len(RemoteScraperAddr) > 0 {
		remoteClient := http.Client{Transport: transportNoProxy, Timeout: timeout}
		req, err := http.NewRequest("GET", RemoteScraperAddr+"/scrape/"+i.PostID, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept-Encoding", "zstd.dict")
		res, err := remoteClient.Do(req)
		if err == nil && res != nil {
			defer res.Body.Close()
			remoteData, err := io.ReadAll(res.Body)
			if err == nil && res.StatusCode == 200 {
				remoteDecomp, err := remoteZSTDReader.DecodeAll(remoteData, nil)
				if err != nil {
					return err
				}
				if err := binary.Unmarshal(remoteDecomp, i); err == nil {
					if len(i.Username) > 0 {
						slog.Info("Data parsed from remote scraper", "postID", i.PostID)
						return nil
					}
				}
			}
			slog.Error("Failed to scrape data from remote scraper", "postID", i.PostID, "status", res.StatusCode, "err", err)
		}
		if err != nil {
			slog.Error("Failed when trying to scrape data from remote scraper", "postID", i.PostID, "err", err)
		}
	}

	client := http.Client{Transport: transport, Timeout: timeout}
	req, err := http.NewRequest("GET", "https://www.instagram.com/p/"+i.PostID+"/embed/captioned/", nil)
	if err != nil {
		return err
	}

	var body []byte
	for retries := 0; retries < 3; retries++ {
		err := func() error {
			res, err := client.Do(req)
			if err != nil {
				return err
			}
			defer res.Body.Close()
			if res.StatusCode != 200 {
				return errors.New("status code is not 200")
			}

			body, err = io.ReadAll(res.Body)
			if err != nil {
				return err
			}
			return nil
		}()
		if err == nil {
			break
		}
	}

	var embedData gjson.Result
	var timeSliceData gjson.Result
	if len(body) > 0 {
		var scriptText []byte

		// TimeSliceImpl (very fragile)
		for _, line := range bytes.Split(body, []byte("\n")) {
			if bytes.Contains(line, []byte("shortcode_media")) {
				scriptText = line
				break
			}
		}

		if len(scriptText) > 0 {
			// Remove <script>
			findFirstMoreThan := bytes.Index(scriptText, []byte(">"))
			scriptText = scriptText[findFirstMoreThan+1:]

			lexer := js.NewLexer(parse.NewInputBytes(scriptText))
			for {
				tt, text := lexer.Next()
				if tt == js.ErrorToken || text == nil {
					break
				}
				if tt == js.StringToken && bytes.Contains(text, []byte("shortcode_media")) {
					// Strip quotes from start and end
					text = text[1 : len(text)-1]
					unescapeData := utils.UnescapeJSONString(utils.B2S(text))
					if !gjson.Valid(unescapeData) {
						slog.Error("Failed to parse data from TimeSliceImpl", "postID", i.PostID, "err", err)
						return err
					}
					timeSliceData = gjson.Parse(unescapeData).Get("gql_data")
				}
			}
		} else {
			slog.Warn("Failed to parse data from TimeSliceImpl", "postID", i.PostID, "err", "No script found")
		}

		// Scrape from embed HTML
		embedHTML, err := scrapeFromEmbedHTML(body)
		if err != nil {
			slog.Warn("Failed to parse data from scrapeFromEmbedHTML", "postID", i.PostID, "err", err)
		} else {
			embedData = gjson.Parse(embedHTML)
		}
	}

	var gqlData gjson.Result
	videoBlocked := bytes.Contains(body, []byte("WatchOnInstagram"))
	// Scrape from GraphQL API only if video is blocked or embed data is empty
	if videoBlocked || len(body) == 0 {
		gqlValue, err := scrapeFromGQL(i.PostID)
		if err != nil {
			slog.Error("Failed to scrape data from scrapeFromGQL", "postID", i.PostID, "err", err)
		}
		if gqlValue != nil && !bytes.Contains(gqlValue, []byte("require_login")) {
			gqlData = gjson.Parse(utils.B2S(gqlValue)).Get("data")
			slog.Info("Data parsed from GraphQL API", "postID", i.PostID)
		}
	}

	// If gqlData is blocked, use timeSliceData or embedData
	if !gqlData.Exists() {
		if timeSliceData.Exists() {
			gqlData = timeSliceData
			slog.Info("Data parsed from TimeSliceImpl", "postID", i.PostID)
		} else {
			gqlData = embedData
			slog.Info("Data parsed from embedHTML", "postID", i.PostID)
		}
	}

	status := gqlData.Get("status").String()
	item := gqlData.Get("shortcode_media")
	if !item.Exists() {
		item = gqlData.Get("xdt_shortcode_media")
		if !item.Exists() {
			if status == "fail" {
				return errors.New("scrapeFromGQL is blocked")
			}
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
	if len(i.Medias) == 0 {
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
	mediaURL, ok := embedMedia.Attr("src")
	if !ok {
		return "", ErrNotFound
	}

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
			"edge_media_to_caption": {"edges": [{"node": {"text": ` + utils.EscapeJSONString(caption) + `}}]},
			"dimensions": {"height": null, "width": null},
			"video_blocked": ` + videoBlocked + `
		}
	}`, nil
}

func scrapeFromGQL(postID string) ([]byte, error) {
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
	req, err := http.NewRequest("POST", "https://www.instagram.com/graphql/query/", strings.NewReader(gqlParams.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header = http.Header{
		"Accept":                      {"*/*"},
		"Accept-Language":             {"en-US,en;q=0.9"},
		"Content-Type":                {"application/x-www-form-urlencoded"},
		"Origin":                      {"https://www.instagram.com"},
		"Priority":                    {"u=1, i"},
		"Sec-Ch-Prefers-Color-Scheme": {"dark"},
		"Sec-Ch-Ua":                   {`"Google Chrome";v="125", "Chromium";v="125", "Not.A/Brand";v="24"`},
		"Sec-Ch-Ua-Full-Version-List": {`"Google Chrome";v="125.0.6422.142", "Chromium";v="125.0.6422.142", "Not.A/Brand";v="24.0.0.0"`},
		"Sec-Ch-Ua-Mobile":            {"?0"},
		"Sec-Ch-Ua-Model":             {`""`},
		"Sec-Ch-Ua-Platform":          {`"macOS"`},
		"Sec-Ch-Ua-Platform-Version":  {`"12.7.4"`},
		"Sec-Fetch-Dest":              {"empty"},
		"Sec-Fetch-Mode":              {"cors"},
		"Sec-Fetch-Site":              {"same-origin"},
		"User-Agent":                  {"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"},
		"X-Asbd-Id":                   {"129477"},
		"X-Bloks-Version-Id":          {"e2004666934296f275a5c6b2c9477b63c80977c7cc0fd4b9867cb37e36092b68"},
		"X-Fb-Friendly-Name":          {"PolarisPostActionLoadPostQueryQuery"},
		"X-Ig-App-Id":                 {"936619743392459"},
	}

	client := http.Client{Transport: transport, Timeout: timeout}
	res, err := client.Do(req)
	if err != nil || res == nil {
		return nil, err
	}
	defer res.Body.Close()
	return io.ReadAll(res.Body)
}
