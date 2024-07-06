package handlers

import (
	"image"
	"image/jpeg"
	scraper "instafix/handlers/scraper"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RyanCarrier/dijkstra/v2"
	"github.com/bamiaux/rez"
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"
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
var timeout = 60 * time.Second

// getHeight returns the height of the rows, imagesWH [w,h]
func getHeight(imagesWH [][]float64, canvasWidth int) float64 {
	var height float64
	for _, image := range imagesWH {
		height += image[0] / image[1]
	}
	return float64(canvasWidth) / height
}

// costFn returns the cost of the row graph thingy
func costFn(imagesWH [][]float64, i, j, canvasWidth, maxRowHeight int) float64 {
	slices := imagesWH[i:j]
	rowHeight := getHeight(slices, canvasWidth)
	return math.Pow(float64(maxRowHeight)-rowHeight, 2)
}

func createGraph(imagesWH [][]float64, start, canvasWidth int) map[int]uint64 {
	results := make(map[int]uint64, len(imagesWH))
	results[start] = 0
	for i := start + 1; i < len(imagesWH); i++ {
		// Max 3 images for every row
		if i-start > 3 {
			break
		}
		results[i] = uint64(costFn(imagesWH, start, i, canvasWidth, 1000))
	}
	return results
}

func avg(n []float64) float64 {
	var sum float64
	for _, v := range n {
		sum += v
	}
	return sum / float64(len(n))
}

// GenerateGrid generates a grid of images
// based on https://blog.vjeux.com/2014/image/google-plus-layout-find-best-breaks.html
func GenerateGrid(images []image.Image) (image.Image, error) {
	var imagesWH [][]float64
	images = append(images, image.Rect(0, 0, 0, 0)) // Needed as for some reason the last image is not added
	for _, image := range images {
		imagesWH = append(imagesWH, []float64{float64(image.Bounds().Dx()), float64(image.Bounds().Dy())})
	}

	// Calculate canvas width by taking the average of width of all images
	// There should be a better way to do this
	var allWidth []float64
	for _, image := range imagesWH {
		allWidth = append(allWidth, image[0])
	}
	canvasWidth := int(avg(allWidth) * 1.5)

	graph := dijkstra.NewGraph()
	for i := range images {
		graph.AddVertexAndArcs(i, createGraph(imagesWH, i, canvasWidth))
	}

	// Get the shortest path from 0 to len(images)-1
	best, err := graph.Shortest(0, len(images)-1)
	if err != nil {
		return nil, err
	}
	path := best.Path

	canvasHeight := 0
	var heightRows []int
	// Calculate height of each row and canvas height
	for i := 1; i < len(path); i++ {
		rowWH := imagesWH[path[i-1]:path[i]]

		rowHeight := int(getHeight(rowWH, canvasWidth))
		heightRows = append(heightRows, rowHeight)
		canvasHeight += rowHeight
	}

	canvas := image.NewYCbCr(image.Rect(0, 0, canvasWidth, canvasHeight), image.YCbCrSubsampleRatio420)

	oldRowHeight := 0
	for i := 1; i < len(path); i++ {
		inRow := images[path[i-1]:path[i]]
		oldImWidth := 0
		heightRow := heightRows[i-1]
		for _, imageOne := range inRow {
			newWidth := float64(heightRow) * float64(imageOne.Bounds().Dx()) / float64(imageOne.Bounds().Dy())
			if err := rez.Convert(canvas.SubImage(image.Rect(oldImWidth, oldRowHeight, oldImWidth+int(newWidth), oldRowHeight+int(heightRow))), imageOne, rez.NewBilinearFilter()); err != nil {
				return nil, err
			}
			oldImWidth += int(newWidth)
		}
		oldRowHeight += heightRow
	}
	return canvas, nil
}

func Grid() fiber.Handler {
	return func(c *fiber.Ctx) error {
		postID := c.Params("postID")
		gridFname := filepath.Join("static", postID+".jpeg")

		// If already exists, return
		if _, ok := scraper.LRU.Get(gridFname); ok {
			return c.SendFile(gridFname)
		}

		item, err := scraper.GetData(postID)
		if err != nil {
			return err
		}

		// Filter media only include image
		var mediaURLs []string
		for _, media := range item.Medias {
			if !strings.Contains(media.TypeName, "Image") {
				continue
			}
			mediaURLs = append(mediaURLs, media.URL)
		}

		if len(item.Medias) == 1 || len(mediaURLs) == 1 {
			return c.Redirect("/images/" + postID + "/1")
		}

		var wg sync.WaitGroup
		images := make([]image.Image, len(mediaURLs))
		client := http.Client{Transport: transport, Timeout: timeout}
		for i, mediaURL := range mediaURLs {
			wg.Add(1)

			go func(i int, url string) {
				defer wg.Done()
				req, err := http.NewRequest(http.MethodGet, url, http.NoBody)
				if err != nil {
					return
				}

				// Make request client.Get
				res, err := client.Do(req)
				if err != nil {
					log.Error().Str("postID", postID).Err(err).Msg("Failed to get image")
					return
				}
				defer res.Body.Close()

				images[i], err = jpeg.Decode(res.Body)
				if err != nil {
					log.Error().Str("postID", postID).Err(err).Msg("Failed to decode image")
					return
				}
			}(i, mediaURL)
		}
		wg.Wait()

		// Create grid Images
		grid, err := GenerateGrid(images)
		if err != nil {
			return err
		}

		// Write grid to static folder
		f, err := os.Create(gridFname)
		if err != nil {
			return err
		}
		defer f.Close()

		if err := jpeg.Encode(f, grid, &jpeg.Options{Quality: 80}); err != nil {
			return err
		}
		scraper.LRU.Add(gridFname, true)
		return c.SendFile(gridFname)
	}
}
