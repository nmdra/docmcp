package crawler_test

import (
	"os"
	"path/filepath"
	"testing"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "sitemap", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}
