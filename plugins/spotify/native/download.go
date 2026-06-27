package native

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/devgianlu/go-librespot/audio"
	downloadpb "github.com/devgianlu/go-librespot/proto/spotify/download"
	extmetadatapb "github.com/devgianlu/go-librespot/proto/spotify/extendedmetadata"
	audiofilespb "github.com/devgianlu/go-librespot/proto/spotify/extendedmetadata/audiofiles"
	metadatapb "github.com/devgianlu/go-librespot/proto/spotify/metadata"
)

// Sentinel errors let the caller decide whether to fall back to the YouTube
// Music delegate. All of these mean "native Spotify audio is unavailable for
// this track" rather than a hard failure.
var (
	// ErrNoVorbisFormat means the track exposes no Ogg Vorbis tier we can
	// decrypt (typically a playplay/DRM-only or FLAC-only entry).
	ErrNoVorbisFormat = errors.New("native: no ogg vorbis format available")
	// ErrAudioKeyRefused means the access point refused the AES audio key,
	// which is how playplay (StopStop) DRM tracks present.
	ErrAudioKeyRefused = errors.New("native: audio key refused (likely drm)")
)

// Format identifies the Ogg Vorbis bitrate tier selected for a download.
type Format struct {
	Bitrate int    // 96, 160, or 320
	Codec   string // always "vorbis" for the formats we request
}

// resolvedStream is a fully decrypted, container-normalized audio stream ready
// to be copied to disk and fed to ffmpeg. Size is the decrypted byte length.
type resolvedStream struct {
	reader io.Reader
	size   int64
	format Format
	closer func() error
}

func (r *resolvedStream) Read(p []byte) (int, error) { return r.reader.Read(p) }

// Size returns the decrypted audio byte length (after the Spotify metadata page
// is stripped).
func (r *resolvedStream) Size() int64 { return r.size }

// Format returns the selected Ogg Vorbis tier.
func (r *resolvedStream) Format() Format { return r.format }

func (r *resolvedStream) Close() error {
	if r != nil && r.closer != nil {
		return r.closer()
	}
	return nil
}

// oggVorbisFormats lists the Ogg Vorbis formats we know how to deliver, best
// first. We deliberately ignore AAC/MP3/FLAC: FLAC needs the proprietary
// playplay key (StopStop DRM) and the AAC/MP3 tiers aren't offered for normal
// accounts via this path. Vorbis 320 is the practical Premium ceiling here.
var oggVorbisFormats = []metadatapb.AudioFile_Format{
	metadatapb.AudioFile_OGG_VORBIS_320,
	metadatapb.AudioFile_OGG_VORBIS_160,
	metadatapb.AudioFile_OGG_VORBIS_96,
}

func formatBitrate(f metadatapb.AudioFile_Format) int {
	switch f {
	case metadatapb.AudioFile_OGG_VORBIS_96:
		return 96
	case metadatapb.AudioFile_OGG_VORBIS_160:
		return 160
	case metadatapb.AudioFile_OGG_VORBIS_320:
		return 320
	default:
		return 0
	}
}

// selectVorbisFile picks the Ogg Vorbis file closest to the preferred bitrate.
// preferredBitrate<=0 means "highest available".
func selectVorbisFile(files []*metadatapb.AudioFile, preferredBitrate int) *metadatapb.AudioFile {
	byFormat := map[metadatapb.AudioFile_Format]*metadatapb.AudioFile{}
	for _, f := range files {
		if f == nil || f.Format == nil {
			continue
		}
		byFormat[*f.Format] = f
	}
	if len(byFormat) == 0 {
		return nil
	}

	// Highest available when no preference.
	if preferredBitrate <= 0 {
		for _, want := range oggVorbisFormats {
			if f, ok := byFormat[want]; ok {
				return f
			}
		}
		return nil
	}

	// Otherwise pick the closest bitrate among the Vorbis tiers we have.
	var best *metadatapb.AudioFile
	bestDist := 1 << 30
	for _, want := range oggVorbisFormats {
		f, ok := byFormat[want]
		if !ok {
			continue
		}
		d := formatBitrate(want) - preferredBitrate
		if d < 0 {
			d = -d
		}
		if best == nil || d < bestDist {
			best = f
			bestDist = d
		}
	}
	return best
}

// download resolves a Spotify track URI to a decrypted, normalized Ogg Vorbis
// stream. preferredBitrate selects the tier (0 = highest available).
//
// The pipeline mirrors player.NewStream but stops at "decrypted Ogg bytes"
// (no PCM decode): metadata -> select format -> AES key -> storage resolve ->
// chunked HTTP reader -> AES-128-CTR decrypt -> strip Spotify's leading Ogg
// page -> standard Ogg Vorbis.
func (s *session) download(ctx context.Context, trackURI string, preferredBitrate int) (*resolvedStream, error) {
	spotID, err := librespot.SpotifyIdFromUri(trackURI)
	if err != nil {
		return nil, fmt.Errorf("native: invalid track uri %q: %w", trackURI, err)
	}
	if spotID.Type() != librespot.SpotifyIdTypeTrack {
		return nil, fmt.Errorf("native: unsupported id type %q (only tracks)", spotID.Type())
	}

	// Resolve track metadata, relinking to a playable alternative if the
	// original is region-restricted for this account's country.
	trackMeta, err := s.unrestrictedTrack(ctx, *spotID)
	if err != nil {
		return nil, err
	}

	media := librespot.NewMediaFromTrack(trackMeta)
	resolvedID := media.Id()

	// Fetch the audio-files extension to learn which formats exist (the inline
	// track.File list is sometimes empty; the extension is authoritative).
	var audioFilesResp audiofilespb.AudioFilesExtensionResponse
	if err := s.sp.ExtendedMetadataSimple(ctx, resolvedID, extmetadatapb.ExtensionKind_AUDIO_FILES, &audioFilesResp); err != nil {
		return nil, fmt.Errorf("native: failed getting audio files metadata: %w", err)
	}

	files := append([]*metadatapb.AudioFile(nil), trackMeta.File...)
	for _, f := range audioFilesResp.Files {
		if f.File != nil {
			files = append(files, f.File)
		}
	}

	file := selectVorbisFile(files, preferredBitrate)
	if file == nil {
		// No Ogg Vorbis tier — likely a playplay/DRM-only track we can't decrypt.
		return nil, ErrNoVorbisFormat
	}

	// Retrieve the AES audio key over the access point. If Spotify has migrated
	// this track to the playplay (StopStop) DRM, this request is refused — the
	// caller falls back to the YouTube Music delegate.
	key, err := s.audioKey.Request(ctx, resolvedID.Id(), file.FileId)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAudioKeyRefused, err)
	}

	// Resolve the CDN URL(s) for the encrypted file.
	storage, err := s.sp.ResolveStorageInteractive(ctx, file.FileId, file.Format, false)
	if err != nil {
		return nil, fmt.Errorf("native: failed resolving storage: %w", err)
	}

	rawStream, err := openChunkedReader(s.log, s.client, storage)
	if err != nil {
		return nil, err
	}

	// AES-128-CTR decrypt (fixed Spotify IV).
	decryptor, err := audio.NewAesAudioDecryptor(rawStream, key)
	if err != nil {
		_ = rawStream.Close()
		return nil, fmt.Errorf("native: failed initializing decryptor: %w", err)
	}

	size := rawStream.Size()

	// Strip Spotify's proprietary leading Ogg page (magic 0x81 packet carrying
	// seek-table + replay-gain) so the saved file is a standard Ogg Vorbis
	// stream ffmpeg reads directly.
	audioStart, err := spotifyOggAudioStart(decryptor, size)
	if err != nil {
		_ = rawStream.Close()
		return nil, err
	}

	// SectionReader over the decryptor (which is a ReaderAt) for the audio body.
	body := io.NewSectionReader(decryptor, audioStart, size-audioStart)

	return &resolvedStream{
		reader: body,
		size:   size - audioStart,
		format: Format{Bitrate: formatBitrate(*file.Format), Codec: "vorbis"},
		closer: rawStream.Close,
	}, nil
}

// unrestrictedTrack loads track metadata and, if the primary recording is
// region-restricted for the account country, swaps in a playable alternative.
func (s *session) unrestrictedTrack(ctx context.Context, id librespot.SpotifyId) (*metadatapb.Track, error) {
	var trackMeta metadatapb.Track
	if err := s.sp.ExtendedMetadataSimple(ctx, id, extmetadatapb.ExtensionKind_TRACK_V4, &trackMeta); err != nil {
		return nil, fmt.Errorf("native: failed getting track metadata: %w", err)
	}

	country := s.country()
	media := librespot.NewMediaFromTrack(&trackMeta)
	if !mediaRestricted(media, country) {
		return &trackMeta, nil
	}

	for _, alt := range trackMeta.Alternative {
		am := librespot.NewMediaFromTrack(alt)
		if !mediaRestricted(am, country) {
			trackMeta.Alternative = nil
			trackMeta.Gid = alt.Gid
			trackMeta.File = alt.File
			trackMeta.Preview = alt.Preview
			trackMeta.OriginalAudio = alt.OriginalAudio
			return &trackMeta, nil
		}
	}
	return nil, librespot.ErrMediaRestricted
}

// country returns the account country code, or "" if unknown. We treat unknown
// as unrestricted; if the track is truly unavailable the AES key request fails
// later and the caller falls back to YTM.
func (s *session) country() string { return "" }

// openChunkedReader builds an HTTP chunked reader over the first usable CDN URL,
// trying each candidate in turn.
func openChunkedReader(log librespot.Logger, client *http.Client, storage *downloadpb.StorageResolveResponse) (*audio.HttpChunkedReader, error) {
	switch storage.Result {
	case downloadpb.StorageResolveResponse_CDN:
		// handled below
	case downloadpb.StorageResolveResponse_STORAGE:
		return nil, fmt.Errorf("native: legacy storage not supported")
	case downloadpb.StorageResolveResponse_RESTRICTED:
		return nil, fmt.Errorf("native: storage is restricted")
	default:
		return nil, fmt.Errorf("native: unknown storage resolve result: %s", storage.Result)
	}
	if len(storage.Cdnurl) == 0 {
		return nil, fmt.Errorf("native: no cdn urls")
	}

	var lastErr error
	for _, raw := range storage.Cdnurl {
		parsed, err := url.Parse(raw)
		if err != nil {
			lastErr = err
			continue
		}
		reader, err := audio.NewHttpChunkedReader(log, client, parsed.String())
		if err != nil {
			lastErr = err
			continue
		}
		return reader, nil
	}
	return nil, fmt.Errorf("native: failed creating chunked reader for any cdn url: %w", lastErr)
}
