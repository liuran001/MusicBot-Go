package applemusic

import (
	"context"
	"fmt"
	"os"

	mp4 "github.com/Eyevinn/mp4ff/mp4"
)

// remuxToProgressive rewrites a fragmented MP4 (the form produced by both the
// native Widevine decrypt and the wrapper cbcs pipeline) into a progressive
// MP4 with a complete moov sample table.
//
// Why this matters: Apple Music streams are fragmented MP4 (ftyp + moov[mvex]
// + repeating moof/mdat). Telegram's inline audio player and many desktop
// players (e.g. Windows) cannot show a duration/progress bar or seek in a
// fragmented MP4 — playback appears broken even though the audio is intact.
//
// We must NOT use ffmpeg `-c copy` for this: ffmpeg's mp4 demuxer only reads
// the first fragment of these files (the moov advertises 0 samples and ffmpeg
// won't walk the moof chain), silently producing a file with just the first
// ~15s. Instead we use the mp4ff library, which correctly reads every fragment,
// and rebuild a flat sample table (stts/stsc/stsz/stco) + single mdat.
//
// The operation is in-place: on success the file at path is replaced with the
// progressive version. The ctx is accepted for signature symmetry; the work is
// CPU/IO-bound and not cancelled mid-write.
func remuxToProgressive(_ context.Context, path string) error {
	in, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	parsed, err := mp4.DecodeFile(in)
	in.Close()
	if err != nil {
		return fmt.Errorf("decode mp4: %w", err)
	}
	if !parsed.IsFragmented() {
		return nil // already progressive
	}
	if parsed.Init == nil || parsed.Init.Moov == nil {
		return fmt.Errorf("fragmented file has no init/moov")
	}

	prog, err := buildProgressiveFile(parsed)
	if err != nil {
		return fmt.Errorf("build progressive: %w", err)
	}

	tmp := path + ".prog.m4a"
	out, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	if err := prog.Encode(out); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("encode progressive: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace original: %w", err)
	}
	return nil
}

// buildProgressiveFile converts a parsed fragmented mp4 into a progressive
// *mp4.File: ftyp + moov (with rebuilt sample tables, mvex removed) + a single
// mdat holding all sample data concatenated in decode order.
//
// Apple Music enhancedHls is single-track audio; we handle the general single
// audio-track case (the only shape these decrypt pipelines produce).
func buildProgressiveFile(frag *mp4.File) (*mp4.File, error) {
	moov := frag.Init.Moov
	traks := moov.Traks
	if len(traks) != 1 {
		return nil, fmt.Errorf("expected 1 track, got %d", len(traks))
	}
	trak := traks[0]
	trex := (*mp4.TrexBox)(nil)
	if moov.Mvex != nil && len(moov.Mvex.Trexs) > 0 {
		trex = moov.Mvex.Trexs[0]
	}

	// Gather all samples (with timing/size) and their raw data across every
	// fragment of every segment, in order.
	var (
		allData    []byte
		sampleSize []uint32
		sampleDur  []uint32
		totalDur   uint64
	)
	for _, seg := range frag.Segments {
		for _, f := range seg.Fragments {
			samples, err := f.GetFullSamples(trex)
			if err != nil {
				return nil, fmt.Errorf("get samples: %w", err)
			}
			for i := range samples {
				s := &samples[i]
				allData = append(allData, s.Data...)
				sampleSize = append(sampleSize, s.Size)
				sampleDur = append(sampleDur, s.Dur)
				totalDur += uint64(s.Dur)
			}
		}
	}
	if len(sampleSize) == 0 {
		return nil, fmt.Errorf("no samples found in fragments")
	}

	// Rebuild the sample table boxes from the gathered samples.
	stbl := trak.Mdia.Minf.Stbl

	// stts: run-length encode (count, delta) of per-sample durations.
	stts := &mp4.SttsBox{}
	for _, d := range sampleDur {
		n := len(stts.SampleTimeDelta)
		if n > 0 && stts.SampleTimeDelta[n-1] == d {
			stts.SampleCount[n-1]++
		} else {
			stts.SampleCount = append(stts.SampleCount, 1)
			stts.SampleTimeDelta = append(stts.SampleTimeDelta, d)
		}
	}

	// stsz: explicit per-sample sizes.
	stsz := &mp4.StszBox{
		SampleNumber: uint32(len(sampleSize)),
		SampleSize:   sampleSize,
	}

	// stsc: one chunk holding all samples.
	stsc := &mp4.StscBox{}
	if err := stsc.AddEntry(1, uint32(len(sampleSize)), 1); err != nil {
		return nil, fmt.Errorf("stsc entry: %w", err)
	}

	// stco: single chunk; its absolute offset is patched after we know the
	// moov size (mdat data starts right after ftyp+moov+mdat header).
	stco := &mp4.StcoBox{ChunkOffset: []uint32{0}}

	// Replace the stbl's table boxes with the rebuilt progressive ones, keeping
	// stsd (codec config) intact and dropping any fragment-only boxes.
	rebuildStblChildren(stbl, stts, stsc, stsz, stco)

	// Reflect total duration in mvhd/mdhd/tkhd.
	mdhdTimescale := trak.Mdia.Mdhd.Timescale
	trak.Mdia.Mdhd.Duration = totalDur
	if moov.Mvhd != nil {
		if moov.Mvhd.Timescale == mdhdTimescale {
			moov.Mvhd.Duration = totalDur
		} else if mdhdTimescale > 0 {
			moov.Mvhd.Duration = totalDur * uint64(moov.Mvhd.Timescale) / uint64(mdhdTimescale)
		}
	}
	if trak.Tkhd != nil && moov.Mvhd != nil && mdhdTimescale > 0 {
		trak.Tkhd.Duration = totalDur * uint64(moov.Mvhd.Timescale) / uint64(mdhdTimescale)
	}

	// Remove mvex (fragment declaration) — a progressive file must not have it.
	removeMvex(moov)

	// Assemble the progressive file: ftyp + moov + mdat. AddChild takes the
	// box's byte start position; track it as we append.
	out := mp4.NewFile()
	ftyp := frag.Ftyp
	if ftyp == nil && frag.Init != nil {
		ftyp = frag.Init.Ftyp
	}
	var pos uint64
	if ftyp != nil {
		out.AddChild(ftyp, pos)
		pos += ftyp.Size()
	}
	out.AddChild(moov, pos)
	pos += moov.Size()
	mdat := &mp4.MdatBox{}
	mdat.SetData(allData)
	out.AddChild(mdat, pos)

	// Patch stco: mdat payload starts after ftyp + moov + mdat header.
	stco.ChunkOffset[0] = uint32(pos + mdat.HeaderSize())

	return out, nil
}

// rebuildStblChildren replaces the sample-table boxes in stbl with the rebuilt
// progressive ones, preserving stsd and dropping stale/fragment boxes.
func rebuildStblChildren(stbl *mp4.StblBox, stts *mp4.SttsBox, stsc *mp4.StscBox, stsz *mp4.StszBox, stco *mp4.StcoBox) {
	var kept []mp4.Box
	for _, child := range stbl.Children {
		switch child.Type() {
		case "stsd":
			kept = append(kept, child) // keep codec config
		case "stts", "stsc", "stsz", "stco", "co64", "ctts", "stss", "sgpd", "sbgp":
			// drop: rebuilt below or fragment-only
		default:
			kept = append(kept, child)
		}
	}
	kept = append(kept, stts, stsc, stsz, stco)
	stbl.Children = kept
	stbl.Stts = stts
	stbl.Stsc = stsc
	stbl.Stsz = stsz
	stbl.Stco = stco
}

// removeMvex drops the mvex box from moov (both the typed field and Children).
func removeMvex(moov *mp4.MoovBox) {
	moov.Mvex = nil
	out := moov.Children[:0]
	for _, child := range moov.Children {
		if child.Type() == "mvex" {
			continue
		}
		out = append(out, child)
	}
	moov.Children = out
}
