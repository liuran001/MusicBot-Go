package applemusic

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	mp4 "github.com/Eyevinn/mp4ff/mp4"
)

// This file ports the cbcs sample-decryption pipeline from
// zhaarey/apple-music-downloader's utils/runv2 onto the Eyevinn/mp4ff library.
// It decrypts Apple Music enhancedHls streams (ALAC lossless / Hi-Res / Atmos)
// by streaming each encrypted sample to the external FairPlay wrapper over TCP
// and writing back the plaintext in place.
//
// The wrapper performs the actual FairPlay key exchange and AES; this code is
// the mp4 demux + cbcs block-selection + wire framing around it.
//
// IMPORTANT: the wrapper TCP protocol implemented here is faithfully ported
// from the reference (verified against both runv2.go and agent.js), but it has
// NOT been exercised end-to-end in this repo because no wrapper instance is
// available in the dev environment. The pure-Go parts (mp4 walking, cbcs byte
// selection, framing) are covered by unit tests with a fake wrapper.

const prefetchKeyURI = "skd://itunes.apple.com/P000000000/s1/e1"

// wrapperConn abstracts the bidirectional wrapper connection so the pipeline
// can be unit-tested against an in-memory fake.
type wrapperConn interface {
	io.ReadWriter
}

// sendString writes a length-prefixed string: [1 byte len][bytes].
func sendString(w io.Writer, s string) error {
	if len(s) > 255 {
		return fmt.Errorf("string too long for 1-byte length prefix: %d", len(s))
	}
	if _, err := w.Write([]byte{byte(len(s))}); err != nil {
		return err
	}
	_, err := io.WriteString(w, s)
	return err
}

// switchKeys writes the 4-zero-byte marker that ends the current sample loop
// (used before sending a new key for the next fragment).
func switchKeys(w io.Writer) error {
	_, err := w.Write([]byte{0, 0, 0, 0})
	return err
}

// closeSession writes the 5-zero-byte marker that ends the whole wrapper
// session (the leading zero ends the outer per-track loop on the server).
func closeSession(w io.Writer) error {
	_, err := w.Write([]byte{0, 0, 0, 0, 0})
	return err
}

// cbcsFullSubsampleDecrypt sends an entire (16-byte-aligned) chunk to the
// wrapper and reads back the plaintext in place. Used for ALAC/Atmos audio
// where skip_byte_block == 0 (full-subsample encryption).
func cbcsFullSubsampleDecrypt(data []byte, rw *bufio.ReadWriter) error {
	// Drop the trailing <16 bytes: cbcs leaves the final partial AES block in
	// the clear. Sending a 0-length chunk would be read as the end marker, so
	// skip entirely when there's nothing aligned to send.
	truncatedLen := len(data) & ^0xf
	if truncatedLen == 0 {
		return nil
	}
	if err := binary.Write(rw, binary.LittleEndian, uint32(truncatedLen)); err != nil {
		return err
	}
	if _, err := rw.Write(data[:truncatedLen]); err != nil {
		return err
	}
	if err := rw.Flush(); err != nil {
		return err
	}
	_, err := io.ReadFull(rw, data[:truncatedLen])
	return err
}

// cbcsStripeDecrypt sends only the crypt blocks (every decryptBlockLen bytes,
// skipping skipBlockLen) and reads them back in place. Used for striped
// (pattern) encryption, e.g. AVC/HEVC video. Included for completeness.
func cbcsStripeDecrypt(data []byte, rw *bufio.ReadWriter, decryptBlockLen, skipBlockLen int) error {
	size := len(data)
	if size < decryptBlockLen {
		return nil
	}
	count := ((size - decryptBlockLen) / (decryptBlockLen + skipBlockLen)) + 1
	totalLen := count * decryptBlockLen
	if err := binary.Write(rw, binary.LittleEndian, uint32(totalLen)); err != nil {
		return err
	}
	pos := 0
	for {
		if size-pos < decryptBlockLen {
			break
		}
		if _, err := rw.Write(data[pos : pos+decryptBlockLen]); err != nil {
			return err
		}
		pos += decryptBlockLen
		if size-pos < skipBlockLen {
			break
		}
		pos += skipBlockLen
	}
	if err := rw.Flush(); err != nil {
		return err
	}
	pos = 0
	for {
		if size-pos < decryptBlockLen {
			break
		}
		if _, err := io.ReadFull(rw, data[pos:pos+decryptBlockLen]); err != nil {
			return err
		}
		pos += decryptBlockLen
		if size-pos < skipBlockLen {
			break
		}
		pos += skipBlockLen
	}
	return nil
}

func cbcsDecryptRaw(data []byte, rw *bufio.ReadWriter, decryptBlockLen, skipBlockLen int) error {
	if skipBlockLen == 0 {
		return cbcsFullSubsampleDecrypt(data, rw)
	}
	return cbcsStripeDecrypt(data, rw, decryptBlockLen, skipBlockLen)
}

// cbcsDecryptSample decrypts one cbcs sample in place.
func cbcsDecryptSample(sample []byte, rw *bufio.ReadWriter, subSamples []mp4.SubSamplePattern, tenc *mp4.TencBox) error {
	decryptBlockLen := int(tenc.DefaultCryptByteBlock) * 16
	skipBlockLen := int(tenc.DefaultSkipByteBlock) * 16

	if len(subSamples) == 0 {
		return cbcsDecryptRaw(sample, rw, decryptBlockLen, skipBlockLen)
	}
	var pos uint32
	for _, ss := range subSamples {
		pos += uint32(ss.BytesOfClearData)
		if ss.BytesOfProtectedData == 0 {
			continue
		}
		end := pos + ss.BytesOfProtectedData
		if int(end) > len(sample) {
			return fmt.Errorf("subsample protected range %d-%d exceeds sample len %d", pos, end, len(sample))
		}
		if err := cbcsDecryptRaw(sample[pos:end], rw, decryptBlockLen, skipBlockLen); err != nil {
			return err
		}
		pos = end
	}
	return nil
}

// decryptFragment decrypts every sample in a moof+mdat fragment in place, then
// strips the encryption boxes and fixes trun data offsets.
func decryptFragment(frag *mp4.Fragment, tracks map[uint32]mp4.DecryptTrackInfo, rw *bufio.ReadWriter) error {
	moof := frag.Moof
	var bytesRemoved uint64

	for _, traf := range moof.Trafs {
		ti, ok := tracks[traf.Tfhd.TrackID]
		if !ok {
			return fmt.Errorf("no decryption info for track %d", traf.Tfhd.TrackID)
		}
		if ti.Sinf == nil {
			continue // unencrypted track
		}
		if st := ti.Sinf.Schm.SchemeType; st != "cbcs" {
			return fmt.Errorf("unsupported scheme %q (only cbcs)", st)
		}
		hasSenc, isParsed := traf.ContainsSencBox()
		if !hasSenc {
			return fmt.Errorf("no senc box in traf")
		}
		var senc *mp4.SencBox
		if traf.Senc != nil {
			senc = traf.Senc
		} else {
			senc = traf.UUIDSenc.Senc
		}
		if !isParsed {
			if err := senc.ParseReadBox(ti.Sinf.Schi.Tenc.DefaultPerSampleIVSize, traf.Saiz); err != nil {
				return fmt.Errorf("parse senc: %w", err)
			}
		}
		samples, err := frag.GetFullSamples(ti.Trex)
		if err != nil {
			return fmt.Errorf("get samples: %w", err)
		}
		for i := range samples {
			var subSamples []mp4.SubSamplePattern
			if len(senc.SubSamples) != 0 {
				subSamples = senc.SubSamples[i]
			}
			if err := cbcsDecryptSample(samples[i].Data, rw, subSamples, ti.Sinf.Schi.Tenc); err != nil {
				return fmt.Errorf("decrypt sample %d: %w", i, err)
			}
		}
		bytesRemoved += traf.RemoveEncryptionBoxes()
	}
	_, psshRemoved := moof.RemovePsshs()
	bytesRemoved += psshRemoved
	for _, traf := range moof.Trafs {
		for _, trun := range traf.Truns {
			trun.DataOffset -= int32(bytesRemoved)
		}
	}
	return nil
}

// readInitSegment reads the ftyp+moov init segment from the start of the mp4.
func readInitSegment(r io.Reader) (*mp4.InitSegment, uint64, error) {
	var offset uint64
	init := mp4.NewMP4Init()
	for i := 0; i < 2; i++ {
		box, err := mp4.DecodeBox(offset, r)
		if err != nil {
			return nil, offset, err
		}
		if t := box.Type(); t != "ftyp" && t != "moov" {
			return nil, offset, fmt.Errorf("unexpected box %q, want ftyp/moov", t)
		}
		init.AddChild(box)
		offset += box.Size()
	}
	return init, offset, nil
}

// readNextFragment reads the next moof+mdat fragment. Returns (nil, offset, nil)
// at EOF.
func readNextFragment(r io.Reader, offset uint64) (*mp4.Fragment, uint64, error) {
	frag := mp4.NewFragment()
	for {
		box, err := mp4.DecodeBox(offset, r)
		if err == io.EOF {
			return nil, offset, nil
		}
		if err != nil {
			return nil, offset, err
		}
		offset += box.Size()
		t := box.Type()
		if t == "moof" || t == "emsg" || t == "prft" {
			frag.AddChild(box)
			continue
		}
		if t == "mdat" {
			frag.AddChild(box)
			break
		}
		// ignore unexpected mid-stream boxes
	}
	if frag.Moof == nil {
		return nil, offset, fmt.Errorf("fragment has mdat but no moof (offset %d)", offset)
	}
	return frag, offset, nil
}

// sanitizeInit removes the duplicate ec-3/alac stsd entry (2 exist because of 2
// IVs; they become identical after decryption and some tools reject >1 entry).
func sanitizeInit(init *mp4.InitSegment) error {
	traks := init.Moov.Traks
	if len(traks) != 1 {
		return fmt.Errorf("expected 1 track, got %d", len(traks))
	}
	stsd := traks[0].Mdia.Minf.Stbl.Stsd
	if stsd.SampleCount <= 1 {
		return nil
	}
	if stsd.SampleCount > 2 {
		return fmt.Errorf("expected 1 or 2 stsd entries, got %d", stsd.SampleCount)
	}
	if stsd.Children[0].Type() != stsd.Children[1].Type() {
		return fmt.Errorf("stsd entries differ in type")
	}
	stsd.Children = stsd.Children[:1]
	stsd.SampleCount = 1
	return nil
}

// filterFairPlayKeys keeps only the FairPlay EXT-X-KEY lines (the wrapper needs
// the skd:// URI). Mirrors runv2's filterResponse — m3u8 libraries don't handle
// multiple keys per segment, so PlayReady/Widevine key lines are dropped.
func filterFairPlayKeys(playlist string) string {
	var buf bytes.Buffer
	for _, line := range splitLines(playlist) {
		if hasPrefix(line, "#EXT-X-KEY:") && !containsSub(line, "streamingkeydelivery") {
			continue
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	return buf.String()
}

// small dependency-free string helpers (kept local to avoid importing strings
// in hot byte paths and to keep this file self-contained/testable).
func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if n := len(line); n > 0 && line[n-1] == '\r' {
				line = line[:n-1]
			}
			out = append(out, line)
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
