package applemusic

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
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

// DecryptSamples opens a decrypt session on TCP port 10020 and decrypts audio samples.
//
// Wire protocol:
//
//	CLIENT: [1 byte adamId_len][adamId][1 byte uri_len][skd_uri]
//	Then per sample:
//	  CLIENT: [4 bytes LE sample_size][sample_data]
//	  SERVER: [sample_size bytes decrypted_data]
//	End:
//	  CLIENT: [4 bytes: 0x00000000]
func (w *WrapperClient) DecryptSamples(ctx context.Context, adamId, skdURI string, samples [][]byte) ([][]byte, error) {
	addr := fmt.Sprintf("%s:%d", w.host, w.decryptPort)
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("wrapper decrypt: connect %s: %w", addr, err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	} else {
		conn.SetDeadline(time.Now().Add(120 * time.Second))
	}

	// Session init: adamId
	idBytes := []byte(adamId)
	if _, err := conn.Write([]byte{byte(len(idBytes))}); err != nil {
		return nil, err
	}
	if _, err := conn.Write(idBytes); err != nil {
		return nil, err
	}

	// Session init: skd URI
	uriBytes := []byte(skdURI)
	if _, err := conn.Write([]byte{byte(len(uriBytes))}); err != nil {
		return nil, err
	}
	if _, err := conn.Write(uriBytes); err != nil {
		return nil, err
	}

	// Decrypt each sample.
	decrypted := make([][]byte, 0, len(samples))
	for _, sample := range samples {
		// Send sample size (uint32 LE).
		sizeBuf := make([]byte, 4)
		binary.LittleEndian.PutUint32(sizeBuf, uint32(len(sample)))
		if _, err := conn.Write(sizeBuf); err != nil {
			return nil, err
		}
		// Send sample data.
		if _, err := conn.Write(sample); err != nil {
			return nil, err
		}
		// Read decrypted data (same size).
		dec := make([]byte, len(sample))
		if _, err := io.ReadFull(conn, dec); err != nil {
			return nil, fmt.Errorf("wrapper decrypt: read decrypted sample: %w", err)
		}
		decrypted = append(decrypted, dec)
	}

	// End session.
	endBuf := make([]byte, 4)
	conn.Write(endBuf) // ignore error on close

	return decrypted, nil
}
