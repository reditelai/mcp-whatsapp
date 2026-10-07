package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// OutsideMiladka says why the server will not run here, or "" when it may.
// The add-on belongs to Miládka and runs only from her add-on folder, in a
// folder that has .miladka/VERSION (Karel, 8. 10. 2026). Only release builds
// check it (main.release); a build from source runs anywhere, for development.
func OutsideMiladka() string {
	bin, err := exeDir()
	if err != nil {
		return fmt.Sprintf("nejde zjistit, kde program leží: %v", err)
	}
	return outsideMiladka(bin)
}

func outsideMiladka(bin string) string {
	addons := filepath.Dir(bin)
	if !addonsDirs[filepath.Base(addons)] {
		return "program neleží ve složce doplňků Miládky (.doplnky/mcp-whatsapp/), ale v " + bin
	}
	root := filepath.Dir(addons)
	if fi, err := os.Stat(filepath.Join(root, ".miladka", "VERSION")); err == nil && fi.Mode().IsRegular() {
		return ""
	}
	return "ve složce " + root + " chybí .miladka/VERSION, není to složka Miládky"
}

// MiladkaRequired is what the server says when it will not start outside
// Miládka, the same for every reason: a person outside Miládka needs Miládka,
// not a path (Karel, 8. 10. 2026). Inside Miládka the assistant finds the
// cause from .mcp.json.
func MiladkaRequired() string {
	return "mcp-whatsapp je doplněk Miládky a funguje jen v ní. Pořiďte si Miládku na https://miladka.cz a doplněk si nainstalujte v ní.\n" +
		"mcp-whatsapp is an add-on for Miládka and works only inside her. Get Miládka at https://miladka.cz and install the add-on there."
}
