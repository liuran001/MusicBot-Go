package kugou

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	lyricpkg "github.com/liuran001/MusicBot-Go/bot/lyric"
)

// krcResult holds the word-by-word tracks extracted from a Kugou KRC document.
type krcResult struct {
	RawQRC      string // word timing re-emitted as QRC-style token track
	Lyric       string // line-timed LRC
	Translation string
	Roma        string
}

// fetchKRC retrieves and decrypts Kugou's word-by-word ("逐词") KRC lyric for a
// song hash. It searches krcs.kugou.com for a candidate, downloads the KRC blob
// from lyrics.kugou.com, decrypts it, and parses relative word offsets into
// absolute timing. Returns nil (no error) when no KRC candidate is available so
// callers can fall back to the plain LRC endpoint.
func (c *Client) fetchKRC(ctx context.Context, hash string) (*krcResult, error) {
	hash = strings.ToUpper(strings.TrimSpace(hash))
	if hash == "" {
		return nil, nil
	}

	searchURL := "http://krcs.kugou.com/search?ver=1&man=yes&client=mobi&hash=" + url.QueryEscape(hash) + "&album_audio_id=&duration=&lrctype=&keyword="
	searchBody, err := c.krcHTTPGet(ctx, searchURL)
	if err != nil {
		return nil, err
	}
	var search struct {
		Status     int    `json:"status"`
		Info       string `json:"info"`
		Candidates []struct {
			ID        string `json:"id"`
			AccessKey string `json:"accesskey"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(searchBody, &search); err != nil {
		return nil, fmt.Errorf("kugou: decode krc search: %w", err)
	}
	if len(search.Candidates) == 0 {
		return nil, nil
	}
	cand := search.Candidates[0]
	if strings.TrimSpace(cand.ID) == "" || strings.TrimSpace(cand.AccessKey) == "" {
		return nil, nil
	}

	downloadURL := "http://lyrics.kugou.com/download?ver=1&client=pc&id=" + url.QueryEscape(cand.ID) + "&accesskey=" + url.QueryEscape(cand.AccessKey) + "&fmt=krc&charset=utf8"
	dlBody, err := c.krcHTTPGet(ctx, downloadURL)
	if err != nil {
		return nil, err
	}
	var dl struct {
		Status  int    `json:"status"`
		Content string `json:"content"`
		Fmt     string `json:"fmt"`
	}
	if err := json.Unmarshal(dlBody, &dl); err != nil {
		return nil, fmt.Errorf("kugou: decode krc download: %w", err)
	}
	if strings.TrimSpace(dl.Content) == "" {
		return nil, nil
	}

	text, err := lyricpkg.DecodeKRC(dl.Content)
	if err != nil {
		return nil, fmt.Errorf("kugou: decrypt krc: %w", err)
	}
	parsed := lyricpkg.ParseKRC(text)
	if strings.TrimSpace(parsed.RawQRC) == "" {
		return nil, nil
	}
	return &krcResult{
		RawQRC:      parsed.RawQRC,
		Lyric:       parsed.Lyric,
		Translation: parsed.Translation,
		Roma:        parsed.Roma,
	}, nil
}

func (c *Client) krcHTTPGet(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "*/*")
	resp, err := c.htmlHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}
