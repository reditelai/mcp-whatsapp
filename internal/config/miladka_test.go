package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func vault(t *testing.T, addons string, version bool) string {
	root := t.TempDir()
	bin := filepath.Join(root, addons, "mcp-whatsapp")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if version {
		if err := os.MkdirAll(filepath.Join(root, ".miladka"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".miladka", "VERSION"), []byte("2.0.0\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return bin
}

func TestOutsideMiladka(t *testing.T) {
	for _, addons := range []string{".doplnky", ".addons"} {
		if p := outsideMiladka(vault(t, addons, true)); p != "" {
			t.Errorf("%s: %s", addons, p)
		}
	}
	if p := outsideMiladka(vault(t, ".doplnky", false)); !strings.Contains(p, ".miladka/VERSION") {
		t.Errorf("without VERSION: %q", p)
	}
	if p := outsideMiladka(vault(t, "programy", true)); !strings.Contains(p, "složce doplňků") {
		t.Errorf("outside the add-on folder: %q", p)
	}
}
