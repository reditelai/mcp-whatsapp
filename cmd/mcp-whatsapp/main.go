// Command mcp-whatsapp is a stdio MCP server for WhatsApp. See SPEC.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
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
	if *cfgPath == "" {
		fmt.Fprintln(os.Stderr, "Chybí --config: cesta ke config.json (vzor v config.example.json).")
		os.Exit(2)
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *check {
		owner := "chybí (žádná zpráva není pokyn a hlídač nic neohlásí)"
		if len(cfg.Owners) > 0 {
			owner = "+" + strings.Join(cfg.Owners, ", +")
		}
		fmt.Fprintf(os.Stderr, "Konfigurace %s je v pořádku.\nData: %s\nPřepis hlasovek: %s\nMajitel (owner): %s\nHlídač budí: %s\n",
			cfg.Path, cfg.DataDir, cfg.Transcription.Dir, owner, cfg.WakeSummary())
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
