package audio

import (
	"bytes"
	"context"
	"testing"
)

func TestEncodeAndTriple(t *testing.T) {
	if !Available() {
		t.Skip("ffmpeg not on PATH")
	}
	ctx := context.Background()

	// 0.4s of 24kHz mono s16le PCM (silence is fine for structural checks).
	pcm := make([]byte, int(24000*0.4)*2)
	ogg, err := EncodePCMToOggOpus(ctx, pcm, 24000)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !bytes.HasPrefix(ogg, []byte("OggS")) {
		t.Fatalf("encoded output is not an OGG stream")
	}

	tripled, err := TripleOggWithGap(ctx, ogg, 20)
	if err != nil {
		t.Fatalf("triple: %v", err)
	}
	if !bytes.HasPrefix(tripled, []byte("OggS")) {
		t.Fatalf("tripled output is not an OGG stream")
	}
	// Tripling roughly triples the audio, so the container must grow.
	if len(tripled) <= len(ogg) {
		t.Fatalf("tripled output (%d bytes) not larger than input (%d bytes)", len(tripled), len(ogg))
	}
}
