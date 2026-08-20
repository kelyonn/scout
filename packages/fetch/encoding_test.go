package fetch

import (
	"bytes"
	"compress/gzip"
	"io"
	"testing"

	"github.com/andybalholm/brotli"
)

func TestDecodeBody(t *testing.T) {
	const want = "the quick brown fox jumps over the lazy dog"

	t.Run("identity passes through unchanged", func(t *testing.T) {
		r, err := decodeBody("", bytes.NewBufferString(want))
		if err != nil {
			t.Fatalf("decodeBody: %v", err)
		}
		defer func() { _ = r.Close() }()
		assertDecodes(t, r, want)
	})

	t.Run("explicit identity token", func(t *testing.T) {
		r, err := decodeBody("identity", bytes.NewBufferString(want))
		if err != nil {
			t.Fatalf("decodeBody: %v", err)
		}
		defer func() { _ = r.Close() }()
		assertDecodes(t, r, want)
	})

	t.Run("gzip is decoded", func(t *testing.T) {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		_, _ = zw.Write([]byte(want))
		_ = zw.Close()

		r, err := decodeBody("gzip", &buf)
		if err != nil {
			t.Fatalf("decodeBody: %v", err)
		}
		defer func() { _ = r.Close() }()
		assertDecodes(t, r, want)
	})

	t.Run("malformed gzip is an error, not a panic or garbage output", func(t *testing.T) {
		_, err := decodeBody("gzip", bytes.NewBufferString("not actually gzip"))
		if err == nil {
			t.Fatal("decodeBody(gzip) accepted a non-gzip body without error")
		}
	})

	t.Run("brotli is decoded", func(t *testing.T) {
		// The whole reason this package carries a dependency: Fetch asks for
		// "Accept-Encoding: gzip, br" and net/http has no built-in Brotli
		// decoder, so a server that actually answers with br would otherwise
		// come back as unreadable bytes.
		var buf bytes.Buffer
		bw := brotli.NewWriter(&buf)
		_, _ = bw.Write([]byte(want))
		_ = bw.Close()

		r, err := decodeBody("br", &buf)
		if err != nil {
			t.Fatalf("decodeBody: %v", err)
		}
		defer func() { _ = r.Close() }()
		assertDecodes(t, r, want)
	})

	t.Run("an encoding we never offered passes through rather than erroring", func(t *testing.T) {
		// Degrading gracefully here matches the package's general posture:
		// an unexpected header should not fail the whole poll cycle.
		r, err := decodeBody("deflate", bytes.NewBufferString(want))
		if err != nil {
			t.Fatalf("decodeBody: %v", err)
		}
		defer func() { _ = r.Close() }()
		assertDecodes(t, r, want)
	})
}

func assertDecodes(t *testing.T, r io.Reader, want string) {
	t.Helper()
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read decoded body: %v", err)
	}
	if string(got) != want {
		t.Errorf("decoded body = %q, want %q", got, want)
	}
}
