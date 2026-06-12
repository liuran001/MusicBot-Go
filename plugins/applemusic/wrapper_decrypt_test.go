package applemusic

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"testing"
)

// Real ALAC media (sub) playlist captured from the live API (track 1450695739).
const testALACMediaPlaylist = `#EXTM3U
#EXT-X-TARGETDURATION:15
#EXT-X-VERSION:7
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-PLAYLIST-TYPE:VOD
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-KEY:METHOD=SAMPLE-AES,URI="skd://itunes.apple.com/P000000000/s1/e1",KEYFORMAT="com.apple.streamingkeydelivery",KEYFORMATVERSIONS="1"
#EXT-X-KEY:METHOD=SAMPLE-AES,URI="data:text/plain;charset=UTF-16;base64,AAAA",KEYFORMAT="com.microsoft.playready",KEYFORMATVERSIONS="1"
#EXT-X-KEY:METHOD=SAMPLE-AES,URI="data:text/plain;base64,AAAAOHBzc2g=",KEYFORMAT="urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed",KEYFORMATVERSIONS="1"
#EXT-X-MAP:URI="P1249856578_A1450695739_audio_en_gr2116_alac_m.mp4",BYTERANGE="1020@0"
#EXTINF:14.95365,
#EXT-X-BYTERANGE:2319993@1020
P1249856578_A1450695739_audio_en_gr2116_alac_m.mp4
#EXT-X-KEY:METHOD=SAMPLE-AES,URI="skd://itunes.apple.com/p1249856578/c23",KEYFORMAT="com.apple.streamingkeydelivery",KEYFORMATVERSIONS="1"
#EXT-X-KEY:METHOD=SAMPLE-AES,URI="data:text/plain;base64,AAAAOHBzc2gx",KEYFORMAT="urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed",KEYFORMATVERSIONS="1"
#EXTINF:14.95365,
#EXT-X-BYTERANGE:2681927@2321013
P1249856578_A1450695739_audio_en_gr2116_alac_m.mp4
#EXTINF:14.95365,
#EXT-X-BYTERANGE:2904907@5002940
P1249856578_A1450695739_audio_en_gr2116_alac_m.mp4
`

func TestParseEnhancedHLSMedia(t *testing.T) {
	mediaURL := "https://aod.itunes.apple.com/itunes-assets/HLSMusic211/v4/78/24/f9/x/P1249856578_default.m3u8"
	m, err := parseEnhancedHLSMedia(mediaURL, testALACMediaPlaylist)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	wantMP4 := "https://aod.itunes.apple.com/itunes-assets/HLSMusic211/v4/78/24/f9/x/P1249856578_A1450695739_audio_en_gr2116_alac_m.mp4"
	if m.MP4URL != wantMP4 {
		t.Errorf("MP4URL:\n got  %q\n want %q", m.MP4URL, wantMP4)
	}

	// 3 segments. First uses the prefetch key; the rest use the real key c23.
	if len(m.SegKeys) != 3 {
		t.Fatalf("expected 3 segments, got %d: %v", len(m.SegKeys), m.SegKeys)
	}
	if m.SegKeys[0] != prefetchKeyURI {
		t.Errorf("segment 0 key: got %q want prefetch", m.SegKeys[0])
	}
	for i := 1; i < 3; i++ {
		if m.SegKeys[i] != "skd://itunes.apple.com/p1249856578/c23" {
			t.Errorf("segment %d key: got %q want c23", i, m.SegKeys[i])
		}
	}
}

// fakeWrapper echoes back exactly the bytes it was asked to decrypt (identity
// transform). Used to validate our wire framing + cbcs byte-selection without a
// real wrapper.

func TestCbcsFullSubsampleFraming(t *testing.T) {
	// Simulate one sample of 40 bytes. truncatedLen = 32 (40 & ^0xf).
	sample := make([]byte, 40)
	for i := range sample {
		sample[i] = byte(i + 1)
	}
	orig := append([]byte(nil), sample...)

	// Full-duplex split: client writes to clientToServer, reads from
	// serverToClient. cbcsFullSubsampleDecrypt flushes then ReadFull-s, so we
	// pre-load the server's echo of the first 32 bytes.
	clientToServer := &bytes.Buffer{}
	serverToClient := &bytes.Buffer{}
	rw := bufio.NewReadWriter(bufio.NewReader(serverToClient), bufio.NewWriter(clientToServer))
	serverToClient.Write(orig[:32])

	if err := cbcsFullSubsampleDecrypt(sample, rw); err != nil {
		t.Fatalf("cbcs: %v", err)
	}

	// Verify what the client sent: [4B LE size=32][32 bytes].
	sent := clientToServer.Bytes()
	if len(sent) != 4+32 {
		t.Fatalf("client sent %d bytes, want 36", len(sent))
	}
	gotSize := binary.LittleEndian.Uint32(sent[:4])
	if gotSize != 32 {
		t.Errorf("sent size = %d, want 32", gotSize)
	}
	if !bytes.Equal(sent[4:], orig[:32]) {
		t.Errorf("sent payload mismatch")
	}
	// Verify in-place decrypt: first 32 bytes replaced by echo (identical here),
	// last 8 bytes untouched.
	if !bytes.Equal(sample[:32], orig[:32]) {
		t.Errorf("decrypted region mismatch")
	}
	if !bytes.Equal(sample[32:], orig[32:]) {
		t.Errorf("trailing clear bytes were modified")
	}
}

func TestCbcsSkipsTinySample(t *testing.T) {
	// A sample <16 bytes: truncatedLen==0, nothing should be sent.
	sample := make([]byte, 10)
	clientToServer := &bytes.Buffer{}
	rw := bufio.NewReadWriter(bufio.NewReader(&bytes.Buffer{}), bufio.NewWriter(clientToServer))
	if err := cbcsFullSubsampleDecrypt(sample, rw); err != nil {
		t.Fatalf("cbcs: %v", err)
	}
	rw.Flush()
	if clientToServer.Len() != 0 {
		t.Errorf("expected nothing sent for tiny sample, got %d bytes", clientToServer.Len())
	}
}

func TestSendStringFraming(t *testing.T) {
	var buf bytes.Buffer
	if err := sendString(&buf, "skd://test"); err != nil {
		t.Fatal(err)
	}
	got := buf.Bytes()
	if got[0] != 10 {
		t.Errorf("length prefix = %d, want 10", got[0])
	}
	if string(got[1:]) != "skd://test" {
		t.Errorf("payload = %q", string(got[1:]))
	}
}

func TestSwitchAndCloseMarkers(t *testing.T) {
	var sw bytes.Buffer
	switchKeys(&sw)
	if !bytes.Equal(sw.Bytes(), []byte{0, 0, 0, 0}) {
		t.Errorf("switchKeys = %v, want 4 zeros", sw.Bytes())
	}
	var cl bytes.Buffer
	closeSession(&cl)
	if !bytes.Equal(cl.Bytes(), []byte{0, 0, 0, 0, 0}) {
		t.Errorf("closeSession = %v, want 5 zeros", cl.Bytes())
	}
}

func TestFilterFairPlayKeys(t *testing.T) {
	filtered := filterFairPlayKeys(testALACMediaPlaylist)
	// PlayReady and Widevine key lines must be gone; FairPlay kept.
	if containsSub(filtered, "com.microsoft.playready") {
		t.Error("playready key not filtered")
	}
	if containsSub(filtered, "urn:uuid:edef8ba9") {
		t.Error("widevine key not filtered")
	}
	if !containsSub(filtered, "streamingkeydelivery") {
		t.Error("fairplay key was wrongly removed")
	}
}
