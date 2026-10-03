package app_test

import (
	"bufio"
	"io"
	"testing"
	"time"

	"github.com/nmdra/docmcp/internal/config"
)

// bufReader reads JSON-RPC lines from the server's stdout. Every line must parse
// as JSON: a stray log line would break the protocol, and this reader is where
// that would first show up.
type bufReader struct {
	scanner *bufio.Scanner
}

func newBufReader(r io.Reader) *bufReader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	return &bufReader{scanner: scanner}
}

// ReadLine returns the next stdout line, failing the test if none arrives.
func (b *bufReader) ReadLine(timeout time.Duration) (string, error) {
	type result struct {
		line string
		err  error
	}

	ch := make(chan result, 1)
	go func() {
		if !b.scanner.Scan() {
			err := b.scanner.Err()
			if err == nil {
				err = io.EOF
			}
			ch <- result{err: err}
			return
		}
		ch <- result{line: b.scanner.Text()}
	}()

	select {
	case r := <-ch:
		return r.line, r.err
	case <-time.After(timeout):
		return "", errTimeout
	}
}

// PeekNonBlocking returns any buffered output without waiting, so a test can
// assert that nothing extra reached stdout.
func (b *bufReader) PeekNonBlocking() (string, error) {
	ch := make(chan string, 1)
	go func() {
		if b.scanner.Scan() {
			ch <- b.scanner.Text()
		}
	}()

	select {
	case line := <-ch:
		return line, nil
	case <-time.After(200 * time.Millisecond):
		return "", errTimeout
	}
}

var errTimeout = &timeoutError{}

type timeoutError struct{}

func (e *timeoutError) Error() string { return "timed out reading from the server" }

// seedLibrary indexes a small fixture site so serve has something to answer with.
// The site is served from an httptest server that is then closed, proving the MCP
// path reads only the local index.
func seedLibrary(t *testing.T, dataDir string) {
	t.Helper()

	site := newFixtureSite(t)

	cfg := config.Default()
	cfg.Data.Path = dataDir
	cfg.Embedding.Provider = "fake"

	if _, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "fixture", "--version", "1"); err != nil {
		t.Fatalf("seed library: %v", err)
	}

	// Nothing about the fixture site is reachable from the serve process now.
	site.Close()
}
