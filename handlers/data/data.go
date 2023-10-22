package handlers

import (
	"bytes"
	"errors"
	"instafix/utils"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/PurpleSec/escape"
	"github.com/mus-format/mus-go/unsafe"
	"github.com/nutsdb/nutsdb"
	"github.com/rs/zerolog/log"
	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/js"
	"github.com/valyala/bytebufferpool"
	"github.com/valyala/fastjson"
)

var transport = &http.Transport{
	DialContext: (&net.Dialer{
		Timeout: 5 * time.Second,
	}).DialContext,
	TLSHandshakeTimeout:   time.Minute,
	ResponseHeaderTimeout: time.Minute,
	ExpectContinueTimeout: 1 * time.Second,
	// Disable HTTP keep-alives, needed for proxy
	MaxIdleConnsPerHost: -1,
	MaxConnsPerHost:     1000,
	DisableKeepAlives:   true,
	Proxy:               http.ProxyFromEnvironment,
}
var headers = http.Header{
	"Accept":          {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8"},
	"Accept-Language": {"en-US,en;q=0.9"},
	"Accept-Encoding": {"identity"},
	"User-Agent":      {"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko)"},
	"Sec-Fetch-Mode":  {"navigate"},
}
var timeout = 10 * time.Second
var bucket = "cache"

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

func (i *InstaData) Marshal(bb *bytebufferpool.ByteBuffer) {
	n := unsafe.SizeString(i.PostID)
	n += unsafe.SizeString(i.Username)
	n += unsafe.SizeString(i.Caption)
	for _, m := range i.Medias {
		n += unsafe.SizeString(m.TypeName)
		n += unsafe.SizeString(m.URL)
	}
	bb.B = make([]byte, n)
	n = unsafe.MarshalString(i.PostID, bb.B)
	n += unsafe.MarshalString(i.Username, bb.B[n:])
	n += unsafe.MarshalString(i.Caption, bb.B[n:])
	for _, m := range i.Medias {
		n += unsafe.MarshalString(m.TypeName, bb.B[n:])
		n += unsafe.MarshalString(m.URL, bb.B[n:])
	}
	return
}

func (i *InstaData) Unmarshal(bs []byte) (n int, err error) {
	i.PostID, n, err = unsafe.UnmarshalString(bs)
	if err != nil {
		return
	}
	var n1 int
	i.Username, n1, err = unsafe.UnmarshalString(bs[n:])
	n += n1
	if err != nil {
		return
	}
	i.Caption, n1, err = unsafe.UnmarshalString(bs[n:])
	n += n1
	if err != nil {
		return
	}
	for n < len(bs) {
		var m Media
		m.TypeName, n1, err = unsafe.UnmarshalString(bs[n:])
		n += n1
		if err != nil {
			return
		}
		m.URL, n1, err = unsafe.UnmarshalString(bs[n:])
		n += n1
		if err != nil {
			return
		}
		i.Medias = append(i.Medias, m)
	}
	return
}

func (i *InstaData) GetData(postID string) error {
	var cacheInstaData []byte
	err := DB.View(func(tx *nutsdb.Tx) error {
		e, err := tx.Get(bucket, utils.S2B(postID))
		if err != nil {
			return err
		}
		cacheInstaData = e.Value
		return nil
	})

	if err != nil && errors.Is(err, nutsdb.ErrBucketNotFound) {
		log.Error().Str("postID", postID).Err(err).Msg("Failed to get data from cache")
		return err
	}

	if len(cacheInstaData) > 0 {
		_, err := i.Unmarshal(cacheInstaData)
		if err != nil {
			return err
		}
		log.Info().Str("postID", postID).Msg("Data parsed from cache")
		return nil
	}

	// Get data from Instagram
	data, err := getData(postID)
	if err != nil {
		log.Error().Str("postID", postID).Err(err).Msg("Failed to get data from Instagram")
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
	i.Username = utils.B2S(item.GetStringBytes("owner", "username"))

	// Get caption
	i.Caption = utils.B2S(item.GetStringBytes("edge_media_to_caption", "edges", "0", "node", "text"))

	// Get medias
	for _, m := range media {
		if m.Exists("node") {
			m = m.Get("node")
		}
		mediaURL := m.GetStringBytes("video_url")
		if mediaURL == nil {
			mediaURL = m.GetStringBytes("display_url")
		}
		i.Medias = append(i.Medias, Media{
			TypeName: utils.B2S(m.GetStringBytes("__typename")),
			URL:      utils.B2S(mediaURL),
		})
	}

	err = DB.Update(func(tx *nutsdb.Tx) error {
		// Save data to cache
		bb := bytebufferpool.Get()
		defer bytebufferpool.Put(bb)
		i.Marshal(bb)
		if err != nil {
			log.Error().Str("postID", postID).Err(err).Msg("Failed to marshal data")
			return err
		}
		return tx.Put(bucket, utils.S2B(postID), bb.B, 24*60*60)
	})
	if err != nil {
		log.Error().Str("postID", postID).Err(err).Msg("Failed to save data to cache")
		return err
	}
	return nil
}

func getData(postID string) (*fastjson.Value, error) {
	client := http.Client{Transport: transport, Timeout: timeout}

	req, err := http.NewRequest(http.MethodGet, "https://www.instagram.com/p/"+postID+"/embed/captioned/", nil)
	if err != nil {
		return nil, err
	}
	req.Header = headers

	// Make request client.Get
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	embedContent, err := io.ReadAll(res.Body)

	// Pattern matching using LDE
	l := &Line{}
	var p fastjson.Parser

	// TimeSliceImpl
	ldeMatch := false
	for _, line := range bytes.Split(embedContent, utils.S2B("\n")) {
		// Check if line contains TimeSliceImpl
		ldeMatch, _ = l.Extract(line)
	}

	if ldeMatch {
		timeSlice := append(utils.S2B("(["), l.GetTimeSliceImplValue()...)
		lexer := js.NewLexer(parse.NewInputBytes(timeSlice))
		for {
			tt, text := lexer.Next()
			if tt == js.ErrorToken {
				break
			}
			if tt == js.StringToken && bytes.Contains(text, utils.S2B("shortcode_media")) {
				// Strip quotes from start and end
				text = text[1 : len(text)-1]
				unescapeData := utils.UnescapeJSONString(utils.B2S(text))
				timeSlice, err := p.Parse(unescapeData)
				if err != nil {
					return nil, err
				}
				log.Info().Str("postID", postID).Msg("Data parsed from TimeSliceImpl")
				return timeSlice.Get("gql_data"), nil
			}
		}
	}

	embedHTML, err := ParseEmbedHTML(embedContent)
	if err != nil {
		return nil, err
	}
	embedHTMLData, err := p.ParseBytes(embedHTML)
	if err != nil {
		return nil, err
	}
	log.Info().Str("postID", postID).Msg("Data parsed from ParseEmbedHTML")
	return embedHTMLData, nil
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
	caption := doc.Find(".Caption").Text()

	// Totally safe 100% valid JSON 👍
	return utils.S2B(`{
		"shortcode_media": {
			"owner": {"username": "` + username + `"},
			"node": {"__typename": "` + typename + `", "display_url": "` + mediaURL + `"},
			"edge_media_to_caption": {"edges": [{"node": {"text": ` + escape.JSON(caption) + `}}]},
			"dimensions": {"height": null, "width": null}
		}
	}`), nil
}
