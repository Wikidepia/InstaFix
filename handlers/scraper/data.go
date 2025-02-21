package handlers

import (
	"bytes"
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
)

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
	transport = gzhttp.Transport(http.DefaultTransport, gzhttp.TransportAlwaysDecompress(true))
	transportNoProxy = http.DefaultTransport.(*http.Transport).Clone()
	transportNoProxy.Proxy = nil // Skip any proxy
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

func (i *InstaData) parseGQL(gqlGJSON gjson.Result) error {
	status := gqlGJSON.Get("status").String()
	item := gqlGJSON.Get("shortcode_media")
	if !item.Exists() {
		item = gqlGJSON.Get("xdt_shortcode_media")
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

	if len(media) == 0 {
		return ErrNotFound
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

func (i *InstaData) parseTimeSliceImpl(embedBody []byte) error {
	var scriptText []byte

	// TimeSliceImpl (very fragile)
	for _, line := range bytes.Split(embedBody, []byte("\n")) {
		if bytes.Contains(line, []byte("shortcode_media")) {
			scriptText = line
			break
		}
	}

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
				return errors.New("failed to parse data from TimeSliceImpl")
			}
			return i.parseGQL(gjson.Parse(unescapeData).Get("gql_data"))
		}
	}
	return errors.New("TimeSliceImpl not found in embed")
}

func (i *InstaData) scrapeFromGQL(postID string) error {
	gqlParams := url.Values{
		"variables":         {"{\"shortcode\":\"" + postID + "\",\"fetch_tagged_user_count\":null,\"hoisted_comment_id\":null,\"hoisted_reply_id\":null}"},
		"server_timestamps": {"true"},
		"doc_id":            {"8845758582119845"},
	}
	req, err := http.NewRequest("POST", "https://www.instagram.com/graphql/query/", strings.NewReader(gqlParams.Encode()))
	if err != nil {
		return err
	}
	reqHeader := http.Header{}
	reqHeader.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:128.0) Gecko/20100101 Firefox/128.0")
	reqHeader.Set("Accept", "*/*")
	reqHeader.Set("Accept-Language", "en-US,en;q=0.5")
	reqHeader.Set("Content-Type", "application/x-www-form-urlencoded")
	reqHeader.Set("X-FB-Friendly-Name", "PolarisPostActionLoadPostQueryQuery")
	reqHeader.Set("Origin", "https://www.instagram.com")
	reqHeader.Set("DNT", "1")
	reqHeader.Set("Sec-GPC", "1")
	reqHeader.Set("Connection", "keep-alive")
	reqHeader.Set("Sec-Fetch-Dest", "empty")
	reqHeader.Set("Sec-Fetch-Mode", "cors")
	reqHeader.Set("Sec-Fetch-Site", "same-origin")
	reqHeader.Set("Pragma", "no-cache")
	reqHeader.Set("Cache-Control", "no-cache")
	reqHeader.Set("TE", "trailers")
	req.Header = reqHeader

	client := http.Client{Transport: transport, Timeout: timeout}
	res, err := client.Do(req)
	if err != nil || res == nil {
		return err
	}
	defer res.Body.Close()
	resVal, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	if bytes.Contains(resVal, []byte("require_login")) {
		return ErrNotFound
	}
	return i.parseGQL(gjson.ParseBytes(resVal).Get("data"))
}

func (i *InstaData) parseEmbedHTML(embedHTML []byte) error {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(embedHTML))
	if err != nil {
		return err
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
		return ErrNotFound
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

	i.Username = username
	i.Caption = caption

	i.Medias = append(i.Medias, Media{
		TypeName: typename,
		URL:      mediaURL,
	})
	return nil
}

func (i *InstaData) ScrapeData() error {
	// Scrape from remote scraper if available
	if len(RemoteScraperAddr) > 0 {
		remoteClient := http.Client{Transport: transportNoProxy, Timeout: timeout}
		req, err := http.NewRequest("GET", RemoteScraperAddr+"/scrape/"+i.PostID, nil)
		if err != nil {
			return err
		}
		res, err := remoteClient.Do(req)
		if err == nil && res != nil {
			defer res.Body.Close()
			remoteData, err := io.ReadAll(res.Body)
			if err != nil {
				return err
			}
			if res.StatusCode == 200 {
				if err := binary.Unmarshal(remoteData, i); err == nil {
					slog.Info("Data parsed from remote scraper", "postID", i.PostID)
					return nil
				}
			}
		}
		slog.Error("Failed to scrape data from remote scraper", "postID", i.PostID, "status", res.StatusCode, "err", err)
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

	err = i.parseTimeSliceImpl(body)
	if err != nil {
		slog.Error("Failed to parse data from parseTimeSliceImpl", "postID", i.PostID, "err", err)
	}

	videoBlocked := bytes.Contains(body, []byte("WatchOnInstagram"))
	// Scrape from GraphQL API only if video is blocked or embed data is empty
	if videoBlocked || len(body) == 0 {
		err := i.scrapeFromGQL(i.PostID)
		if err != nil {
			slog.Error("Failed to scrape data from scrapeFromGQL", "postID", i.PostID, "err", err)
		}
	}

	// Scrape from embed HTML
	err = i.parseEmbedHTML(body)
	if err != nil {
		slog.Error("Failed to parse data from scrapeFromEmbedHTML", "postID", i.PostID, "err", err)
	}
	return nil
}

func GetData(postID string) (*InstaData, error) {
	switch postID[0] {
	case 'C', 'D', 'B':
		break
	default:
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
