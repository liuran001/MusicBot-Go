package native

import (
	"bytes"
	"testing"
)

// buildOggPage assembles a minimal valid Ogg page: 27-byte fixed header +
// segment table + body. The body length is encoded across 255-valued lacing
// values plus a final remainder, matching the Ogg spec.
func buildOggPage(t *testing.T, headerType byte, seq uint32, body []byte) []byte {
	t.Helper()

	// Build the segment table for body length using 255-lacing.
	var segTable []byte
	remaining := len(body)
	for remaining >= 255 {
		segTable = append(segTable, 255)
		remaining -= 255
	}
	segTable = append(segTable, byte(remaining))

	page := make([]byte, 0, 27+len(segTable)+len(body))
	page = append(page, 'O', 'g', 'g', 'S') // capture pattern
	page = append(page, 0)                  // version
	page = append(page, headerType)         // header type flag
	page = append(page, make([]byte, 8)...) // granule position
	page = append(page, make([]byte, 4)...) // serial number
	// page sequence number (little-endian)
	page = append(page, byte(seq), byte(seq>>8), byte(seq>>16), byte(seq>>24))
	page = append(page, make([]byte, 4)...) // CRC
	page = append(page, byte(len(segTable)))
	page = append(page, segTable...)
	page = append(page, body...)
	return page
}

func TestSpotifyOggAudioStart(t *testing.T) {
	// First (Spotify proprietary) page: body begins with 0x81 magic.
	metaBody := append([]byte{0x81}, bytes.Repeat([]byte{0xAB}, 300)...)
	firstPage := buildOggPage(t, 0x02, 0, metaBody)

	// Second page: the real Vorbis identification header.
	secondBody := append([]byte{0x01, 'v', 'o', 'r', 'b', 'i', 's'}, bytes.Repeat([]byte{0x00}, 20)...)
	secondPage := buildOggPage(t, 0x00, 1, secondBody)

	stream := append(append([]byte{}, firstPage...), secondPage...)

	start, err := spotifyOggAudioStart(bytes.NewReader(stream), int64(len(stream)))
	if err != nil {
		t.Fatalf("spotifyOggAudioStart returned error: %v", err)
	}
	if start != int64(len(firstPage)) {
		t.Fatalf("audio start = %d, want %d (first page length)", start, len(firstPage))
	}

	// The byte at the computed offset must be the start of the second "OggS" page.
	if !bytes.Equal(stream[start:start+4], []byte("OggS")) {
		t.Fatalf("offset %d does not point at an OggS page", start)
	}
}

func TestSpotifyOggAudioStartRejectsNonOgg(t *testing.T) {
	junk := bytes.Repeat([]byte{0x00}, 64)
	if _, err := spotifyOggAudioStart(bytes.NewReader(junk), int64(len(junk))); err == nil {
		t.Fatal("expected error for non-ogg stream, got nil")
	}
}

func TestSpotifyOggAudioStartRejectsBadMagic(t *testing.T) {
	// Valid Ogg framing but the first packet does not carry Spotify's 0x81 magic.
	body := append([]byte{0x42}, bytes.Repeat([]byte{0x00}, 50)...)
	firstPage := buildOggPage(t, 0x02, 0, body)
	secondPage := buildOggPage(t, 0x00, 1, bytes.Repeat([]byte{0x00}, 10))
	stream := append(append([]byte{}, firstPage...), secondPage...)

	if _, err := spotifyOggAudioStart(bytes.NewReader(stream), int64(len(stream))); err == nil {
		t.Fatal("expected error for missing 0x81 magic, got nil")
	}
}
