// Command mcp-whatsapp is a stdio MCP server for WhatsApp. See SPEC.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/mark3labs/mcp-go/server"

	mcpwhatsapp "github.com/reditelai/mcp-whatsapp"
	"github.com/reditelai/mcp-whatsapp/internal/config"
	"github.com/reditelai/mcp-whatsapp/internal/tools"
	"github.com/reditelai/mcp-whatsapp/internal/wa"
)

func main() {
	cfgPath := flag.String("config", "", "cesta ke config.json (povinné)")
	check := flag.Bool("check", false, "jen zkontroluje konfiguraci a skončí")
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
		fmt.Fprintf(os.Stderr, "Konfigurace %s je v pořádku. Data: %s\n", cfg.Path, cfg.DataDir)
		return
	}

	logger := wa.NewLogger(wa.ParseLevel(os.Getenv("MCP_WHATSAPP_LOG")))
	m, err := wa.NewManager(cfg, logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	// Connecting can take a while; the MCP handshake must not wait for it.
	go m.Start(ctx)
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
