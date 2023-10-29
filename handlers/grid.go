package handlers

import (
	"bytes"
	data "instafix/handlers/data"
	"instafix/utils"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/davidbyttow/govips/v2/vips"
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"
	"go.uber.org/ratelimit"
)

var transport = &http.Transport{
	DialContext: (&net.Dialer{
		Timeout: 5 * time.Second,
	}).DialContext,
	TLSHandshakeTimeout:   5 * time.Second,
	ResponseHeaderTimeout: 5 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
	DisableKeepAlives:     true,
}
var timeout = 10 * time.Second
var rl = ratelimit.New(2)

func Grid() fiber.Handler {
	return func(c *fiber.Ctx) error {
		postID := c.Params("postID")

		// If already exists, return
		if _, err := os.Stat("static/" + postID + ".webp"); err == nil {
			return c.SendFile("static/" + postID + ".webp")
		}

		// Get data
		item := &data.InstaData{}
		err := item.GetData(postID)
		if err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}

		// Only get first 4 images
		if len(item.Medias) == 1 {
			return c.Redirect("/images/" + postID + "/1")
		}

		// Rate limit generation to 2 per second
		rl.Take()

		var images []*vips.ImageRef
		var wg sync.WaitGroup
		var mutex sync.Mutex

		mediaList := item.Medias[:min(4, len(item.Medias))]
		client := http.Client{Transport: transport, Timeout: timeout}
		for _, media := range mediaList {
			// Skip if not image
			if !bytes.Contains(media.TypeName, []byte("Image")) {
				continue
			}
			wg.Add(1)

			go func(media data.Media) {
				defer wg.Done()
				req, err := http.NewRequest(http.MethodGet, utils.B2S(media.URL), nil)
				if err != nil {
					return
				}

				// Make request client.Get
				res, err := client.Do(req)
				if err != nil {
					return
				}

				defer res.Body.Close()
				buf := new(bytes.Buffer)
				_, err = io.Copy(buf, res.Body)
				if err != nil {
					return
				}

				image, err := vips.NewImageFromBuffer(buf.Bytes())
				if err != nil {
					log.Error().Str("postID", postID).Err(err).Msg("Failed to create image from buffer")
					return
				}
				buf.Reset()

				// Append image
				mutex.Lock()
				defer mutex.Unlock()
				images = append(images, image)
			}(media)
		}
		wg.Wait()

		defer func() {
			for _, image := range images {
				image.Close()
			}
		}()

		if len(images) == 0 {
			return c.SendStatus(fiber.StatusNotFound)
		} else if len(images) == 1 {
			return c.Redirect("/images/" + postID + "/1")
		}

		// Join images
		stem := images[0]
		defer stem.Close()
		err = stem.ArrayJoin(images[1:], 2)
		if err != nil {
			log.Error().Str("postID", postID).Err(err).Msg("Failed to join images")
			return c.SendStatus(fiber.StatusInternalServerError)
		}

		// Export to static/ folder
		imagesBuf, _, err := stem.ExportWebp(nil)
		if err != nil {
			log.Error().Str("postID", postID).Err(err).Msg("Failed to export grid image")
			return c.SendStatus(fiber.StatusInternalServerError)
		}

		// SAVE imagesBuf to static/ folder
		f, err := os.Create("static/" + postID + ".webp")
		if err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		defer f.Close()
		f.Write(imagesBuf)

		return c.SendFile("static/" + postID + ".webp")
	}
}
