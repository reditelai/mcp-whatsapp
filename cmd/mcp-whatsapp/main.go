// Command mcp-whatsapp is a stdio MCP server for WhatsApp. See SPEC.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/mark3labs/mcp-go/server"

	mcpwhatsapp "github.com/reditelai/mcp-whatsapp"
	"github.com/reditelai/mcp-whatsapp/internal/config"
	"github.com/reditelai/mcp-whatsapp/internal/tools"
	"github.com/reditelai/mcp-whatsapp/internal/wa"
	"github.com/reditelai/mcp-whatsapp/internal/watch"
)

// release is set by the release build (-ldflags "-X main.release=yes"): only
// a released binary insists on running inside Miládka.
var release string

func main() {
	cfgPath := flag.String("config", "", "cesta ke config.json (povinné)")
	check := flag.Bool("check", false, "jen zkontroluje konfiguraci a skončí")
	wait := flag.Bool("wait", false, "hlídání: počká na novou zprávu a skončí (spouští asistent na pozadí, SPEC.md, Hlídání)")
	cursor := flag.String("cursor", "", "pro --wait: kurzor z posledního wa_new_messages")
	maxRun := flag.Duration("max", watch.Defaults.MaxRun, "pro --wait: nejdelší hlídání, pak konec s kódem 4 (spusť znovu)")
	showVersion := flag.Bool("version", false, "vypíše verzi")
	flag.Parse()

	if *showVersion {
		fmt.Println(mcpwhatsapp.Version())
		return
	}
	// In the waiting mode the model reads stdout and the exit code: a bad
	// start is code 6 there, not a crash-like 1 or 2 that means "start again".
	fail := func(code int, msg string) {
		if *wait {
			fmt.Println("chyba: " + msg)
			os.Exit(watch.ExitUsage)
		}
		fmt.Fprintln(os.Stderr, msg)
		os.Exit(code)
	}
	if release == "yes" {
		if config.OutsideMiladka() != "" {
			fail(1, config.MiladkaRequired())
		}
	}
	if *cfgPath == "" {
		fail(2, "Chybí --config: cesta ke config.json (vzor v config.example.json).")
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fail(1, err.Error())
	}
	if *check {
		owner := "chybí (žádná zpráva není pokyn a hlídač nic neohlásí)"
		if cfg.Wake != "owner" {
			owner = "chybí (žádná zpráva není pokyn; v pořádku, když je asistent na tvém čísle)"
		}
		if len(cfg.Owners) > 0 {
			owner = "+" + strings.Join(cfg.Owners, ", +")
		}
		fmt.Fprintf(os.Stderr, "Konfigurace %s je v pořádku.\nData: %s\nPřepis hlasovek: %s\nMajitel (owner): %s\nHlídač budí: %s\n",
			cfg.Path, cfg.DataDir, cfg.Transcription.Dir, owner, cfg.WakeSummary())
		if w := syncedFolder(cfg.DataDir); w != "" {
			fmt.Fprintf(os.Stderr, "POZOR: data leží ve složce, kterou synchronizuje %s. Klíče k WhatsAppu (session.db) by odešly do cloudu a databáze na synchronizované složce se může poškodit. Přesuň složku Miládky mimo ni.\n", w)
		}
		return
	}
	if *wait {
		c, err := strconv.ParseInt(*cursor, 10, 64)
		if err != nil || c < 0 {
			fmt.Println("chyba: --wait potřebuje --cursor, kurzor z posledního wa_new_messages")
			os.Exit(watch.ExitUsage)
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		o := watch.Defaults
		o.MaxRun = *maxRun
		code := watch.Run(ctx, cfg, c, o, os.Stdout)
		cancel()
		os.Exit(code)
	}

	logger := wa.NewLogger(wa.ParseLevel(os.Getenv("MCP_WHATSAPP_LOG")))
	m, err := wa.NewManager(cfg, logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	// Start only decides about the lock; connecting runs in the background,
	// so the MCP handshake does not wait for it.
	m.Start(ctx)
	defer m.Close()

	s := server.NewMCPServer("mcp-whatsapp", mcpwhatsapp.Version(),
		server.WithToolCapabilities(false),
		server.WithInstructions(tools.Instructions),
	)
	tools.Register(s, m)
	if err := server.ServeStdio(s, server.WithErrorLogger(log.New(os.Stderr, "mcp: ", log.LstdFlags))); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

// syncedFolder names the cloud sync client whose folder holds dir, or "".
func syncedFolder(dir string) string {
	d := strings.ToLower(filepath.ToSlash(dir))
	switch {
	case strings.Contains(d, "/onedrive"):
		return "OneDrive"
	case strings.Contains(d, "/library/mobile documents/") || strings.Contains(d, "/icloud"):
		return "iCloud"
	case strings.Contains(d, "/dropbox/"):
		return "Dropbox"
	case strings.Contains(d, "/google drive/") || strings.Contains(d, "/googledrive/"):
		return "Google Drive"
	}
	return ""
}
