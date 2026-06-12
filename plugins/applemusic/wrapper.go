package applemusic

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	mp4 "github.com/Eyevinn/mp4ff/mp4"
)

// WrapperClient communicates with the WorldObservationLog/wrapper service
// using its raw TCP protocol for m3u8 retrieval and sample decryption.
type WrapperClient struct {
	host        string // e.g. "127.0.0.1"
	decryptPort int    // default 10020
	m3u8Port    int    // default 20020
	accountPort int    // default 30020
}

// NewWrapperClient creates a wrapper client with the given host and default ports.
func NewWrapperClient(host string) *WrapperClient {
	if host == "" {
		host = "127.0.0.1"
	}
	return &WrapperClient{
		host:        host,
		decryptPort: 10020,
		m3u8Port:    20020,
		accountPort: 30020,
	}
}

// GetM3U8URL fetches the HLS m3u8 URL for a given adamId via TCP port 20020.
//
// Wire protocol:
//
//	CLIENT: [1 byte len][N bytes adamId]
//	SERVER: m3u8_url + "\n"
func (w *WrapperClient) GetM3U8URL(ctx context.Context, adamId string) (string, error) {
	addr := fmt.Sprintf("%s:%d", w.host, w.m3u8Port)
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", fmt.Errorf("wrapper m3u8: connect %s: %w", addr, err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	} else {
		conn.SetDeadline(time.Now().Add(30 * time.Second))
	}

	// Send: [1 byte len][adamId bytes]
	idBytes := []byte(adamId)
	if len(idBytes) > 255 {
		return "", fmt.Errorf("wrapper m3u8: adamId too long")
	}
	if _, err := conn.Write([]byte{byte(len(idBytes))}); err != nil {
		return "", err
	}
	if _, err := conn.Write(idBytes); err != nil {
		return "", err
	}

	// Read until newline.
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("wrapper m3u8: read response: %w", err)
	}

	url := strings.TrimSpace(line)
	if url == "" {
		return "", fmt.Errorf("wrapper m3u8: empty response for adamId %s", adamId)
	}
	return url, nil
}

// DecryptEnhancedHLS opens a decrypt session on the wrapper's TCP port and
// decrypts a full enhancedHls mp4 (ALAC / Hi-Res / Atmos) using the FairPlay
// cbcs protocol, writing the cleartext mp4 to w.
//
// The encrypted mp4 is a single byte-range file; segments carry per-fragment
// FairPlay keys (the first fragment uses the prefetch key). This mirrors
// runv2's downloadAndDecryptFile.
//
// Wire protocol (per the reference wrapper, verified against runv2.go + agent.js):
//
//	Per fragment:
//	  (non-first) [4 bytes 0x00000000]            (SwitchKeys: end prev sample loop)
//	  [1 byte adamId_len][adamId]                 (adamId, or "0" for prefetch key)
//	  [1 byte uri_len][skd_uri]                   (FairPlay key URI)
//	Per sample (cbcs):
//	  CLIENT [4 bytes LE size][size bytes ciphertext]
//	  SERVER [size bytes plaintext]
//	Session end:
//	  [5 bytes 0x0000000000]                      (closeSession)
func (w *WrapperClient) DecryptEnhancedHLS(ctx context.Context, adamID string, encMP4 io.Reader, segKeys []string, out io.Writer) error {
	addr := fmt.Sprintf("%s:%d", w.host, w.decryptPort)
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("wrapper decrypt: connect %s: %w", addr, err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	} else {
		conn.SetDeadline(time.Now().Add(300 * time.Second))
	}

	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	if err := decryptMP4ViaWrapper(adamID, encMP4, segKeys, rw, out); err != nil {
		return err
	}
	// Close the wrapper session cleanly.
	if err := closeSession(conn); err != nil {
		return fmt.Errorf("wrapper decrypt: close session: %w", err)
	}
	return nil
}

// decryptMP4ViaWrapper walks the init + fragments of an encrypted mp4, sending
// each fragment's key handshake and cbcs samples to rw, and writes the
// decrypted mp4 to out. Separated from the TCP setup so it is unit-testable
// against an in-memory fake wrapper.
func decryptMP4ViaWrapper(adamID string, encMP4 io.Reader, segKeys []string, rw *bufio.ReadWriter, out io.Writer) error {
	r := bufio.NewReader(encMP4)
	init, offset, err := readInitSegment(r)
	if err != nil {
		return fmt.Errorf("read init: %w", err)
	}
	tracks, err := transformInit(init)
	if err != nil {
		return fmt.Errorf("transform init: %w", err)
	}
	if err := sanitizeInit(init); err != nil {
		// non-fatal; some tracks legitimately have a single stsd entry
		_ = err
	}
	if err := init.Encode(out); err != nil {
		return fmt.Errorf("encode init: %w", err)
	}

	for i := 0; ; i++ {
		frag, newOffset, err := readNextFragment(r, offset)
		if err != nil {
			return fmt.Errorf("read fragment %d: %w", i, err)
		}
		if frag == nil {
			break // EOF
		}
		offset = newOffset

		// Each fragment must have a corresponding segment key from the media
		// playlist (mirrors runv2's playlistSegments[i] alignment guard). A
		// fragment with no key would desync the wrapper's per-sample loop.
		if i >= len(segKeys) {
			return fmt.Errorf("fragment %d has no segment key (have %d keys); playlist/mp4 out of sync", i, len(segKeys))
		}

		// Send the key handshake for this fragment.
		keyURI := segKeys[i]
		if keyURI != "" {
			if i != 0 {
				if err := switchKeys(rw); err != nil {
					return fmt.Errorf("switch keys: %w", err)
				}
			}
			adam := adamID
			if keyURI == prefetchKeyURI {
				adam = "0"
			}
			if err := sendString(rw, adam); err != nil {
				return fmt.Errorf("send adamId: %w", err)
			}
			if err := sendString(rw, keyURI); err != nil {
				return fmt.Errorf("send keyURI: %w", err)
			}
		}

		if err := decryptFragment(frag, tracks, rw); err != nil {
			return fmt.Errorf("decrypt fragment %d: %w", i, err)
		}
		if err := frag.Encode(out); err != nil {
			return fmt.Errorf("encode fragment %d: %w", i, err)
		}
	}
	return nil
}

// transformInit extracts per-track decryption info and removes encryption-only
// sample-group boxes (sbgp/sgpd of type seig/seam) from the init segment.
func transformInit(init *mp4.InitSegment) (map[uint32]mp4.DecryptTrackInfo, error) {
	di, err := mp4.DecryptInit(init)
	tracks := make(map[uint32]mp4.DecryptTrackInfo, len(di.TrackInfos))
	for _, ti := range di.TrackInfos {
		tracks[ti.TrackID] = ti
	}
	if err != nil {
		return tracks, err
	}
	for _, trak := range init.Moov.Traks {
		stbl := trak.Mdia.Minf.Stbl
		stbl.Children = filterSbgpSgpd(stbl.Children)
	}
	return tracks, nil
}

// filterSbgpSgpd drops encryption-related sbgp/sgpd boxes (grouping type seig
// or seam), leaving others (e.g. roll) intact.
func filterSbgpSgpd(children []mp4.Box) []mp4.Box {
	out := make([]mp4.Box, 0, len(children))
	for _, child := range children {
		switch box := child.(type) {
		case *mp4.SbgpBox:
			if box.GroupingType == "seam" || box.GroupingType == "seig" {
				continue
			}
		case *mp4.SgpdBox:
			if box.GroupingType == "seam" || box.GroupingType == "seig" {
				continue
			}
		}
		out = append(out, child)
	}
	return out
}
