package handlers

import (
	"errors"
	"image"
	"image/jpeg"
	scraper "instafix/handlers/scraper"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RyanCarrier/dijkstra/v2"
	"github.com/go-chi/chi/v5"
	"golang.org/x/image/draw"
	"golang.org/x/sync/singleflight"
)

var timeout = 60 * time.Second
var transport = &http.Transport{
	Proxy: nil, // Skip any proxy
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          100,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
}
var sflightGrid singleflight.Group

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
		if len(imagesWH) < path[i-1] {
			return nil, errors.New("imagesWH is not long enough")
		}
		rowWH := imagesWH[path[i-1]:path[i]]

		rowHeight := int(getHeight(rowWH, canvasWidth))
		heightRows = append(heightRows, rowHeight)
		canvasHeight += rowHeight
	}

	canvas := image.NewRGBA(image.Rect(0, 0, canvasWidth, canvasHeight))

	oldRowHeight := 0
	for i := 1; i < len(path); i++ {
		inRow := images[path[i-1]:path[i]]
		oldImWidth := 0
		if len(heightRows) < i {
			return nil, errors.New("heightRows is not long enough")
		}
		heightRow := heightRows[i-1]
		for _, imageOne := range inRow {
			newWidth := float64(heightRow) * float64(imageOne.Bounds().Dx()) / float64(imageOne.Bounds().Dy())
			draw.ApproxBiLinear.Scale(canvas, image.Rect(oldImWidth, oldRowHeight, oldImWidth+int(newWidth), oldRowHeight+int(heightRow)), imageOne, imageOne.Bounds(), draw.Src, nil)
			oldImWidth += int(newWidth)
		}
		oldRowHeight += heightRow
	}
	return canvas, nil
}

func Grid(w http.ResponseWriter, r *http.Request) {
	postID := chi.URLParam(r, "postID")
	gridFname := filepath.Join("static", postID+".jpeg")

	// If already exists, return from cache
	if _, ok := scraper.LRU.Get(gridFname); ok {
		f, err := os.Open(gridFname)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		} else if err == nil {
			defer f.Close()
			w.Header().Set("Content-Type", "image/jpeg")
			io.Copy(w, f)
			return
		}
	}

	item, err := scraper.GetData(postID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
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
		http.Redirect(w, r, "/images/"+postID+"/1", http.StatusFound)
		return
	}

	_, err, _ = sflightGrid.Do(postID, func() (interface{}, error) {
		var wg sync.WaitGroup
		images := make([]image.Image, len(mediaURLs))
		for i, mediaURL := range mediaURLs {
			wg.Add(1)

			go func(i int, url string) {
				defer wg.Done()
				client := http.Client{Transport: transport, Timeout: timeout}
				req, err := http.NewRequest(http.MethodGet, url, http.NoBody)
				if err != nil {
					return
				}

				// Make request client.Get
				res, err := client.Do(req)
				if err != nil {
					slog.Error("Failed to get image", "postID", postID, "err", err)
					return
				}
				defer res.Body.Close()

				images[i], err = jpeg.Decode(res.Body)
				if err != nil {
					slog.Error("Failed to decode image", "postID", postID, "err", err)
					return
				}
			}(i, mediaURL)
		}
		wg.Wait()

		// Create grid Images
		grid, err := GenerateGrid(images)
		if err != nil {
			return false, err
		}

		// Write grid to static folder
		f, err := os.Create(gridFname)
		if err != nil {
			return false, err
		}
		defer f.Close()

		if err := jpeg.Encode(f, grid, &jpeg.Options{Quality: 80}); err != nil {
			return false, err
		}
		scraper.LRU.Add(gridFname, true)
		return true, nil
	})

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	f, err := os.Open(gridFname)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/jpeg")
	io.Copy(w, f)
}
