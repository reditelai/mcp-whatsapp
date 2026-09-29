//go:build dev

// Command devstt installs the transcription engine into a data folder and
// transcribes one voice note (development helper: go run -tags dev).
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/reditelai/mcp-whatsapp/internal/stt"
	"github.com/reditelai/mcp-whatsapp/internal/wa"
)

func main() {
	e := stt.New(filepath.Join(os.Args[1], "stt"), true, 2, 2, wa.NewLogger(wa.LevelInfo))
	if err := e.Install(nil); err != nil {
		panic(err)
	}
	for !e.Ready() {
		st := e.Status()
		if st.State == stt.StateError {
			panic(st.Error)
		}
		fmt.Println(st.State, st.Progress)
		time.Sleep(20 * time.Second)
	}
	t := time.Now()
	text, err := e.Transcribe(context.Background(), os.Args[2])
	if err != nil {
		panic(err)
	}
	fmt.Printf("%.1f s: %s\n", time.Since(t).Seconds(), text)
}
