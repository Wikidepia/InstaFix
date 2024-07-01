package handlers

import (
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	scraper "instafix/handlers/scraper"

	"git.sr.ht/~jackmordaunt/go-libwebp/webp"
	"github.com/RyanCarrier/dijkstra/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/nfnt/resize"
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

// getHeight returns the height of the rows, imagesWH [w,h]
func getHeight(imagesWH [][]float64, canvasWidth float64) float64 {
	var height float64
	for _, image := range imagesWH {
		height += image[0] / image[1]
	}
	return canvasWidth / height
}

// costFn returns the cost of the row graph thingy
func costFn(imagesWH [][]float64, i, j, canvasWidth, maxRowHeight int) float64 {
	slices := imagesWH[i:j]
	rowHeight := getHeight(slices, float64(canvasWidth))
	return math.Pow(math.Abs(rowHeight-float64(maxRowHeight)), 2.0)
}

func createGraph(imagesWH [][]float64, start int) map[int]uint64 {
	results := make(map[int]uint64, len(imagesWH))
	results[start] = 0
	for i := start + 1; i < len(imagesWH); i++ {
		if i-start > 3 {
			break
		}
		results[i] = uint64(costFn(imagesWH, start, i, 4000, 500))
	}
	return results
}

// GenerateGrid generates a grid of images
// based on https://blog.vjeux.com/2014/image/google-plus-layout-find-best-breaks.html
func GenerateGrid(images []image.Image) image.Image {
	var imagesWH [][]float64
	images = append(images, images[len(images)-1])
	for _, image := range images {
		imagesWH = append(imagesWH, []float64{float64(image.Bounds().Dx()), float64(image.Bounds().Dy())})
	}

	graph := dijkstra.NewGraph()
	for i := 0; i < len(images); i++ {
		graph.AddVertexAndArcs(i, createGraph(imagesWH, i))
	}

	// Get the shortest path from 0 to len(images)-1
	best, err := graph.Shortest(0, len(images)-1)
	if err != nil {
		return nil
	}
	path := best.Path

	canvas := image.NewNRGBA(image.Rect(0, 0, 2000, 4000))

	oldRowHeight := 0
	for i := 1; i < len(path); i++ {
		inRow := images[path[i-1]:path[i]]
		var rowWH [][]float64
		for _, image := range inRow {
			rowWH = append(rowWH, []float64{float64(image.Bounds().Dx()), float64(image.Bounds().Dy())})
		}
		rowHeight := getHeight(rowWH, 2000)

		oldImWidth := 0
		for _, imageOne := range inRow {
			newWidth := rowHeight * float64(imageOne.Bounds().Dx()) / float64(imageOne.Bounds().Dy())
			fmt.Println(newWidth, rowHeight)
			imageOne = resize.Resize(uint(newWidth), uint(rowHeight), imageOne, resize.Bilinear)
			draw.Draw(canvas, image.Rect(int(oldImWidth), int(oldRowHeight), int(oldImWidth)+int(imageOne.Bounds().Dx()), int(oldRowHeight)+int(imageOne.Bounds().Dy())), imageOne, imageOne.Bounds().Min, draw.Src)
			oldImWidth += int(newWidth)
		}
		oldRowHeight += int(rowHeight)
	}

	return canvas
}

func Grid() fiber.Handler {
	return func(c *fiber.Ctx) error {
		postID := c.Params("postID")
		gridFname := filepath.Join("static", postID+".webp")

		// If already exists, return
		if _, err := os.Stat(gridFname); err == nil {
			return c.SendFile(gridFname)
		}

		// Get data
		item := &scraper.InstaData{PostID: postID}
		err := item.GetData()
		if err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}

		// Only get first 4 images
		if len(item.Medias) == 1 {
			return c.Redirect("/images/" + postID + "/1")
		}

		// Filter media, only the first 4 image
		var mediaList []scraper.Media
		for i, media := range item.Medias {
			if !strings.Contains(media.TypeName, "Image") {
				continue
			}
			mediaList = append(mediaList, item.Medias[i])
		}

		images := make([]image.Image, len(mediaList))
		var wg sync.WaitGroup

		dirname, err := os.MkdirTemp("static", postID+"*")
		if err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		defer os.RemoveAll(dirname)

		client := http.Client{Transport: transport, Timeout: timeout}
		for i, media := range mediaList {
			wg.Add(1)

			go func(i int, media scraper.Media) {
				defer wg.Done()
				req, err := http.NewRequest(http.MethodGet, media.URL, http.NoBody)
				if err != nil {
					return
				}

				// Make request client.Get
				res, err := client.Do(req)
				if err != nil {
					return
				}
				defer res.Body.Close()

				images[i], err = jpeg.Decode(res.Body)
				if err != nil {
					return
				}
			}(i, media)
		}
		wg.Wait()

		// Create grid Images
		grid := GenerateGrid(images)
		if grid == nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}

		// Write grid to static folder
		f, err := os.Create(gridFname)
		if err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		defer f.Close()

		if err := webp.Encode(f, grid, webp.Quality(0.85)); err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		return c.SendFile(gridFname)
	}
}
