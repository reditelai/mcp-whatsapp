//go:build dev

// Command devdecode converts one Ogg/Opus or WAV file to segmented WAVs
// (development helper for the transcription tests).
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"

	"github.com/reditelai/mcp-whatsapp/internal/audio"
)

func main() {
	in, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	var pcm []int16
	if strings.HasSuffix(os.Args[1], ".wav") {
		in.Seek(44, 0)
		st, _ := in.Stat()
		pcm = make([]int16, (st.Size()-44)/2)
		binary.Read(in, binary.LittleEndian, pcm)
	} else if pcm, err = audio.OggOpusToPCM(in); err != nil {
		panic(err)
	}
	for i, seg := range audio.Segment(pcm, 15, 25) {
		out, _ := os.Create(fmt.Sprintf("%s-%02d.wav", os.Args[2], i))
		audio.WriteWAV(out, seg)
		out.Close()
		fmt.Printf("%.1f ", audio.Duration(seg))
	}
	fmt.Println()
}
