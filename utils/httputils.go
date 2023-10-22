package utils

import (
	"bytes"
	"io"
	"net/http"
)

// ReadBody reads the body of the given response into a byte slice.
// Originally from https://github.com/arangodb/go-driver (ArangoDB GmbH)
// Apache-2.0 License
func ReadBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	contentLength := resp.ContentLength
	if contentLength < 0 {
		// Don't know the content length, do it the slowest way
		result, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		return result, nil
	}
	buf := &bytes.Buffer{}
	if int64(int(contentLength)) == contentLength {
		// contentLength is an int64. If we can safely cast to int, use Grow.
		buf.Grow(int(contentLength))
	}
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
