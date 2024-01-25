package handlers

import (
	"bytes"
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

	"git.sr.ht/~jackmordaunt/go-libwebp/webp"
	"github.com/gofiber/fiber/v2"
	gim "github.com/ozankasikci/go-image-merge"
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

func Grid() fiber.Handler {
	return func(c *fiber.Ctx) error {
		postID := c.Params("postID")
		gridFname := filepath.Join("static", postID+".webp")

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

		// Filter media, only the first 4 image
		mediaList := make([]data.Media, 0, 4)
		for i, media := range item.Medias {
			if !bytes.Contains(media.TypeName, []byte("Image")) {
				continue
			}
			if len(mediaList) == cap(mediaList) {
				break
			}
			mediaList = append(mediaList, item.Medias[i])
		}

		images := make([]string, len(mediaList))
		var wg sync.WaitGroup

		dirname, err := os.MkdirTemp("static", postID+"*")
		if err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		defer os.RemoveAll(dirname)

		client := http.Client{Transport: transport, Timeout: timeout}
		for i, media := range mediaList {
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
				defer res.Body.Close()

				fname := filepath.Join(dirname, strconv.Itoa(i)+".jpg")
				file, err := os.Create(fname)
				if err != nil {
					return
				}

				_, err = io.Copy(file, res.Body)
				if err != nil {
					return
				}

				images[i] = fname
			}(i, media)
		}
		wg.Wait()

		// Create grid Images
		var gridIm []*gim.Grid
		for _, image := range images {
			if image == "" {
				continue
			}
			gridIm = append(gridIm, &gim.Grid{
				ImageFilePath: image,
			})
		}

		if len(gridIm) == 0 {
			return c.SendStatus(fiber.StatusNotFound)
		} else if len(gridIm) == 1 {
			return c.Redirect("/images/" + postID + "/1")
		}

		countY := 1
		if len(gridIm) > 2 {
			countY = 2
		}
		grid, err := gim.New(gridIm, 2, countY).Merge()
		if err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}

		// Write grid to static folder
		f, err := os.Create(gridFname)
		if err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		defer f.Close()

		if err := webp.Encode(f, grid, webp.Quality(0.75)); err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		return c.SendFile(gridFname)
	}
}
