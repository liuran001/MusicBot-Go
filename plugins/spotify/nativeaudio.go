package spotify

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/plugins/spotify/native"
)

// directAudioSource is the surface SpotifyPlatform needs from the native
// librespot-based audio path. It is satisfied by *nativeSource (wrapping
// native.Client) and kept as a local interface so the platform depends on a
// behaviour, not a concrete client, and so it can be stubbed in tests.
type directAudioSource interface {
	// Available reports whether native audio is usable (operator has logged in).
	Available() bool
	// BuildDownloadInfo resolves a Spotify track to a DownloadInfo whose
	// Downloader streams decrypted Ogg Vorbis to disk. It returns a sentinel
	// error the caller can treat as "fall back to the delegate".
	BuildDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error)
}

// nativeSource adapts a *native.Client to directAudioSource.
type nativeSource struct {
	client *native.Client
}

// newNativeSource wraps a native.Client. A nil client yields a source that
// always reports unavailable.
func newNativeSource(client *native.Client) *nativeSource {
	return &nativeSource{client: client}
}

func (n *nativeSource) Available() bool {
	return n != nil && n.client != nil && n.client.Authenticated()
}

// qualityToBitrate maps the unified quality tiers onto the Ogg Vorbis bitrate
// tiers Spotify offers via this path. Lossless/Hi-Res are not attainable
// (FLAC needs the proprietary playplay key), so they map to the 320 ceiling.
func qualityToBitrate(q platform.Quality) int {
	switch q {
	case platform.QualityStandard:
		return 160
	case platform.QualityHigh, platform.QualityLossless, platform.QualityHiRes:
		return 320
	default:
		return 0 // highest available
	}
}

// BuildDownloadInfo resolves the track to a decrypted Ogg Vorbis stream and
// wraps it in a DownloadInfo. The actual network fetch + decrypt happens lazily
// inside the Downloader closure so a failure there (DRM, restriction) can still
// fall back to the delegate at download time.
func (n *nativeSource) BuildDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	if !n.Available() {
		return nil, native.ErrNotAuthenticated
	}

	bitrate := qualityToBitrate(quality)

	downloadFn := func(ctx context.Context, info *platform.DownloadInfo, destPath string, progress func(written, total int64)) (int64, error) {
		stream, err := n.client.Download(ctx, trackID, bitrate)
		if err != nil {
			return 0, err
		}
		defer stream.Close()

		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return 0, err
		}
		f, err := os.Create(destPath)
		if err != nil {
			return 0, err
		}

		total := stream.Size()
		written, copyErr := copyWithProgress(f, stream, total, progress)
		if cerr := f.Close(); cerr != nil && copyErr == nil {
			copyErr = cerr
		}
		if copyErr != nil {
			_ = os.Remove(destPath)
			return 0, copyErr
		}
		if progress != nil {
			progress(written, written)
		}
		return written, nil
	}

	// URL is a non-fetchable sentinel: the download service rejects an empty
	// URL before consulting Downloader, but never fetches it when Downloader is
	// set. We encode the track so logs are meaningful.
	return &platform.DownloadInfo{
		URL:        fmt.Sprintf("spotify-native:track:%s", trackID),
		Format:     "ogg",
		Bitrate:    bitrate,
		Quality:    quality,
		Downloader: downloadFn,
	}, nil
}

// copyWithProgress copies src→dst, reporting progress periodically. total may be
// 0 when unknown.
func copyWithProgress(dst io.Writer, src io.Reader, total int64, progress func(written, total int64)) (int64, error) {
	buf := make([]byte, 64*1024)
	var written int64
	for {
		nr, rerr := src.Read(buf)
		if nr > 0 {
			nw, werr := dst.Write(buf[:nr])
			written += int64(nw)
			if werr != nil {
				return written, werr
			}
			if nw < nr {
				return written, io.ErrShortWrite
			}
			if progress != nil {
				progress(written, total)
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				return written, nil
			}
			return written, rerr
		}
	}
}
