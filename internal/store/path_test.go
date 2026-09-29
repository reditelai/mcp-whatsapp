package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOddFolderName(t *testing.T) {
	for _, name := range []string{"Karel #1", "50% hotovo", "co?", "Miládka", "a%25b"} {
		dir := filepath.Join(t.TempDir(), name)
		os.MkdirAll(dir, 0o700)
		p := filepath.Join(dir, "app.db")
		s, err := Open(p)
		if err != nil {
			t.Errorf("%q: %v", name, err)
			continue
		}
		s.Close()
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%q: database not at the path: %v", name, err)
		}
	}
}
