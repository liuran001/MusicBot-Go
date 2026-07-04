package spotify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/plugins/spotify/native"
)

const spotifyLyricsURL = "https://spclient.wg.spotify.com/color-lyrics/v2/track/"

// GetLyrics fetches Spotify's web-player lyrics. This endpoint uses the same
// sp_dc-derived bearer/client-token pair as Pathfinder and does not require
// Spotify Web API application access.
func (c *Client) GetLyrics(ctx context.Context, trackID string) (*platform.Lyrics, error) {
	trackID = strings.TrimSpace(trackID)
	if trackID == "" {
		return nil, platform.NewNotFoundError(platformName, "track", trackID)
	}
	if c == nil || c.webAuth == nil {
		return nil, platform.NewAuthRequiredError(platformName)
	}

	auth, err := c.webAuth(ctx)
	if err != nil || strings.TrimSpace(auth.Bearer) == "" {
		return nil, platform.NewAuthRequiredError(platformName)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spotifyLyricsURL+url.PathEscape(trackID), nil)
	if err != nil {
		return nil, err
	}
	native.ApplyWebAuthHeaders(req, auth)

	httpClient := c.httpClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden, http.StatusNotFound:
		return nil, platform.NewUnavailableError(platformName, "lyrics", trackID)
	case http.StatusUnauthorized:
		return nil, platform.NewAuthRequiredError(platformName)
	case http.StatusTooManyRequests:
		return nil, platform.NewRateLimitedError(platformName)
	default:
		return nil, fmt.Errorf("spotify: lyrics status %d", resp.StatusCode)
	}

	var result spotifyLyricsResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("spotify: decode lyrics: %w", err)
	}
	lyrics := convertSpotifyLyrics(result)
	if lyrics == nil || strings.TrimSpace(lyrics.Plain) == "" {
		return nil, platform.NewUnavailableError(platformName, "lyrics", trackID)
	}
	return lyrics, nil
}

func convertSpotifyLyrics(result spotifyLyricsResponse) *platform.Lyrics {
	plainLines := make([]string, 0, len(result.Lyrics.Lines))
	timestamped := make([]platform.LyricLine, 0, len(result.Lyrics.Lines))
	synced := !strings.EqualFold(strings.TrimSpace(result.Lyrics.SyncType), "UNSYNCED")

	for _, line := range result.Lyrics.Lines {
		words := strings.TrimSpace(line.Words)
		if words == "" {
			continue
		}
		plainLines = append(plainLines, words)
		if !synced {
			continue
		}
		startMs, err := strconv.ParseInt(strings.TrimSpace(line.StartTimeMs), 10, 64)
		if err != nil || startMs < 0 {
			continue
		}
		timestamped = append(timestamped, platform.LyricLine{
			Time: time.Duration(startMs) * time.Millisecond,
			Text: words,
		})
	}

	if len(plainLines) == 0 {
		return nil
	}
	return &platform.Lyrics{
		Plain:       strings.Join(plainLines, "\n"),
		Timestamped: timestamped,
	}
}
