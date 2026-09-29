// Package audio turns WhatsApp voice notes (Ogg/Opus) into the 16 kHz mono
// WAV that speech recognition engines read. Pure Go, so the server stays one
// binary without ffmpeg.
package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/pion/opus"
	"github.com/pion/opus/pkg/oggreader"
)

// SampleRate of the produced WAV.
const SampleRate = 16000

// maxFrame is the largest Opus packet in samples at 16 kHz (120 ms).
const maxFrame = SampleRate * 120 / 1000

// OggOpusToPCM decodes an Ogg/Opus stream to 16 kHz mono 16-bit samples.
func OggOpusToPCM(r io.Reader) ([]int16, error) {
	ogg, header, err := oggreader.NewWith(r)
	if err != nil {
		return nil, fmt.Errorf("not an Ogg/Opus file: %w", err)
	}
	dec, err := opus.NewDecoderWithOutput(SampleRate, 1)
	if err != nil {
		return nil, err
	}
	// The Ogg ID header is already consumed; the comment header follows.
	var pcm []int16
	frame := make([]int16, maxFrame)
	for {
		packet, _, err := ogg.ParseNextPacket()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading Ogg: %w", err)
		}
		if bytes.HasPrefix(packet, []byte("OpusTags")) || bytes.HasPrefix(packet, []byte("OpusHead")) {
			continue
		}
		n, err := dec.DecodeToInt16(packet, frame)
		if err != nil {
			return nil, fmt.Errorf("decoding Opus: %w", err)
		}
		pcm = append(pcm, frame[:n]...)
	}
	// Pre-skip is given at 48 kHz: encoder delay to drop from the start.
	skip := int(header.PreSkip) * SampleRate / 48000
	if skip > len(pcm) {
		skip = len(pcm)
	}
	return pcm[skip:], nil
}

// WriteWAV writes 16 kHz mono 16-bit PCM as a WAV file.
func WriteWAV(w io.Writer, pcm []int16) error {
	data := uint32(len(pcm) * 2)
	hdr := []any{
		[4]byte{'R', 'I', 'F', 'F'}, 36 + data, [4]byte{'W', 'A', 'V', 'E'},
		[4]byte{'f', 'm', 't', ' '}, uint32(16), uint16(1), uint16(1),
		uint32(SampleRate), uint32(SampleRate * 2), uint16(2), uint16(16),
		[4]byte{'d', 'a', 't', 'a'}, data,
	}
	for _, v := range hdr {
		if err := binary.Write(w, binary.LittleEndian, v); err != nil {
			return err
		}
	}
	return binary.Write(w, binary.LittleEndian, pcm)
}

// Duration of PCM at SampleRate, in seconds.
func Duration(pcm []int16) float64 { return float64(len(pcm)) / SampleRate }

// Segment splits speech into pieces of at most maxSec seconds, cutting in
// the quietest stretch near the target length. Speech recognition models
// transcribe short pieces better and with less memory than one long file:
// a two-minute voice note in one pass came out visibly worse (29. 9. 2026).
func Segment(pcm []int16, targetSec, maxSec float64) [][]int16 {
	const frame = SampleRate * 30 / 1000 // 30 ms
	target := int(targetSec * SampleRate)
	max := int(maxSec * SampleRate)
	if len(pcm) <= max {
		return [][]int16{pcm}
	}
	energy := func(from int) float64 {
		to := from + frame
		if to > len(pcm) {
			to = len(pcm)
		}
		var sum float64
		for _, s := range pcm[from:to] {
			f := float64(s)
			sum += f * f
		}
		if to == from {
			return 0
		}
		return sum / float64(to-from)
	}
	var out [][]int16
	start := 0
	for len(pcm)-start > max {
		// Look for the quietest 300 ms between target and max from start.
		best, bestE := start+max, -1.0
		for pos := start + target; pos+10*frame <= start+max; pos += frame {
			var e float64
			for k := 0; k < 10; k++ {
				e += energy(pos + k*frame)
			}
			if bestE < 0 || e < bestE {
				best, bestE = pos+5*frame, e
			}
		}
		out = append(out, pcm[start:best])
		start = best
	}
	return append(out, pcm[start:])
}
