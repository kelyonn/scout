package fetch

import (
	"compress/gzip"
	"fmt"
	"io"

	"github.com/andybalholm/brotli"
)

// decodeBody wraps body in a decompressor matching contentEncoding, or returns
// it unwrapped for identity or an unrecognized encoding.
//
// This exists because Fetch sends its own Accept-Encoding header (docs/06
// section 4 and 5 both specify "Accept-Encoding: gzip, br"), and net/http's
// automatic transparent gzip decoding — the thing that would normally make
// this unnecessary — is documented to disable itself the moment a caller sets
// Accept-Encoding explicitly. Having asked for br in that header, decoding it
// is this package's job; Go's standard library has no Brotli decoder, which is
// the entire reason github.com/andybalholm/brotli is a dependency here.
func decodeBody(contentEncoding string, body io.Reader) (io.ReadCloser, error) {
	switch contentEncoding {
	case "gzip":
		zr, err := gzip.NewReader(body)
		if err != nil {
			return nil, fmt.Errorf("open gzip reader: %w", err)
		}
		return zr, nil
	case "br":
		return io.NopCloser(brotli.NewReader(body)), nil
	case "", "identity":
		return io.NopCloser(body), nil
	default:
		// An encoding we did not offer and cannot decode. Passing the raw bytes
		// through — rather than erroring — matches this package's general
		// posture of degrading a fetch into unusable content flagged for the
		// caller to notice (e.g. a validation failure on the resulting parse)
		// rather than failing the whole poll cycle over one unexpected header.
		return io.NopCloser(body), nil
	}
}
