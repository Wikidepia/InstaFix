package handlers

import (
	"bytes"
	"image/jpeg"
	data "instafix/handlers/data"
	"instafix/utils"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	gim "github.com/ozankasikci/go-image-merge"
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
var rl = ratelimit.New(1)

func Grid() fiber.Handler {
	return func(c *fiber.Ctx) error {
		postID := c.Params("postID")
		gridFname := filepath.Join("static", postID+".jpeg")

		// If already exists, return
		if _, err := os.Stat(gridFname); err == nil {
			return c.SendFile(gridFname)
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

		// Rate limit generation to 1 per second
		rl.Take()

		var images []*gim.Grid
		var wg sync.WaitGroup
		var mutex sync.Mutex

		dirname, err := os.MkdirTemp("static", postID+"*")
		if err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		defer os.RemoveAll(dirname)

		mediaList := item.Medias[:min(4, len(item.Medias))]
		client := http.Client{Transport: transport, Timeout: timeout}
		for i, media := range mediaList {
			// Skip if not image
			if !bytes.Contains(media.TypeName, []byte("Image")) {
				continue
			}
			wg.Add(1)

			go func(i int, media data.Media) {
				defer wg.Done()
				req, err := http.NewRequest(http.MethodGet, utils.B2S(media.URL), http.NoBody)
				if err != nil {
					return
				}

				// Make request client.Get
				res, err := client.Do(req)
				if err != nil {
					return
				}

				fname := filepath.Join(dirname, strconv.Itoa(i)+".jpg")
				file, err := os.Create(fname)
				if err != nil {
					return
				}

				defer res.Body.Close()
				_, err = io.Copy(file, res.Body)
				if err != nil {
					return
				}

				// Append image
				mutex.Lock()
				defer mutex.Unlock()
				images = append(images, &gim.Grid{ImageFilePath: fname})
			}(i, media)
		}
		wg.Wait()

		if len(images) == 0 {
			return c.SendStatus(fiber.StatusNotFound)
		} else if len(images) == 1 {
			return c.Redirect("/images/" + postID + "/1")
		}

		countY := 1
		if len(images) > 2 {
			countY = 2
		}
		grid, err := gim.New(images, 2, countY).Merge()
		if err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}

		// Write grid to static folder
		f, err := os.Create(gridFname)
		if err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		defer f.Close()
		jpeg.Encode(f, grid, &jpeg.Options{Quality: 75})

		return c.SendFile(gridFname)
	}
}
