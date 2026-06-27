package native

import (
	"fmt"
	"io"
)

// Spotify wraps its Ogg Vorbis streams with a proprietary leading Ogg page
// whose single packet starts with magic byte 0x81 and carries a seek table,
// replay-gain, and other private metadata. A standard Ogg Vorbis decoder
// (ffmpeg) does not understand that packet, so we must drop the entire first
// page and hand off the remainder, which is a spec-compliant Ogg Vorbis stream
// (starting at the "OggS" page that holds the Vorbis identification header).
//
// go-librespot does this with CGo libogg (vorbis.ExtractMetadataPage); we can't
// import that under CGO_ENABLED=0, so we parse the Ogg page framing directly.
//
// Ogg page layout (https://www.rfc-editor.org/rfc/rfc3533):
//
//	0   "OggS" capture pattern (4 bytes)
//	4   stream_structure_version (1, must be 0)
//	5   header_type_flag (1)
//	6   granule position (8)
//	14  bitstream serial number (4)
//	18  page sequence number (4)
//	22  CRC checksum (4)
//	26  number_page_segments = N (1)
//	27  segment table (N bytes); page body length = sum of these bytes
//
// Total page size = 27 + N + sum(segment table).
const oggHeaderFixedLen = 27

// oggMagic is the Ogg page capture pattern.
var oggMagic = [4]byte{'O', 'g', 'g', 'S'}

// spotifyOggAudioStart returns the byte offset, within the decrypted stream,
// at which the standard Ogg Vorbis audio begins — i.e. immediately after
// Spotify's proprietary first Ogg page.
//
// r must read DECRYPTED bytes. size is the total decrypted length.
func spotifyOggAudioStart(r io.ReaderAt, size int64) (int64, error) {
	// Read the fixed header of the first page.
	hdr := make([]byte, oggHeaderFixedLen)
	if _, err := readAtFull(r, hdr, 0); err != nil {
		return 0, fmt.Errorf("native: failed reading first ogg header: %w", err)
	}
	if hdr[0] != oggMagic[0] || hdr[1] != oggMagic[1] || hdr[2] != oggMagic[2] || hdr[3] != oggMagic[3] {
		return 0, fmt.Errorf("native: not an ogg stream (bad capture pattern %x)", hdr[0:4])
	}
	if hdr[4] != 0 {
		return 0, fmt.Errorf("native: unsupported ogg version %d", hdr[4])
	}

	segCount := int(hdr[26])

	// Read the segment table.
	segTable := make([]byte, segCount)
	if _, err := readAtFull(r, segTable, oggHeaderFixedLen); err != nil {
		return 0, fmt.Errorf("native: failed reading ogg segment table: %w", err)
	}

	bodyLen := 0
	for _, v := range segTable {
		bodyLen += int(v)
	}

	firstPageLen := int64(oggHeaderFixedLen + segCount + bodyLen)
	if firstPageLen <= 0 || firstPageLen >= size {
		return 0, fmt.Errorf("native: implausible first ogg page length %d (size %d)", firstPageLen, size)
	}

	// Sanity check: the first packet of Spotify's metadata page begins with the
	// 0x81 magic byte. If it's absent, the file isn't the expected Spotify Ogg
	// layout — fail loudly rather than emit a corrupt file.
	if bodyLen > 0 {
		first := make([]byte, 1)
		if _, err := readAtFull(r, first, int64(oggHeaderFixedLen+segCount)); err == nil {
			if first[0] != 0x81 {
				return 0, fmt.Errorf("native: unexpected first packet magic %#x (want 0x81)", first[0])
			}
		}
	}

	// Validate that a real Ogg page (the Vorbis identification header) starts
	// exactly where we computed: the next 4 bytes must be "OggS" again.
	next := make([]byte, 4)
	if _, err := readAtFull(r, next, firstPageLen); err != nil {
		return 0, fmt.Errorf("native: failed reading second ogg page: %w", err)
	}
	if next[0] != oggMagic[0] || next[1] != oggMagic[1] || next[2] != oggMagic[2] || next[3] != oggMagic[3] {
		return 0, fmt.Errorf("native: second ogg page not found at offset %d", firstPageLen)
	}

	return firstPageLen, nil
}

// readAtFull reads len(p) bytes at off, retrying short reads, and treating a
// trailing io.EOF as success when the buffer is filled.
func readAtFull(r io.ReaderAt, p []byte, off int64) (int, error) {
	total := 0
	for total < len(p) {
		n, err := r.ReadAt(p[total:], off+int64(total))
		total += n
		if err != nil {
			if err == io.EOF && total == len(p) {
				return total, nil
			}
			return total, err
		}
	}
	return total, nil
}
