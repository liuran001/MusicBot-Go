package dynplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"time"

	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/traefik/yaegi/interp"
)

type pluginMeta struct {
	Name      string         `json:"name"`
	Version   string         `json:"version"`
	URL       string         `json:"url"`
	Platforms []platformInfo `json:"platforms"`
}

type platformInfo struct {
	Name              string                `json:"name"`
	Capabilities      platform.Capabilities `json:"capabilities"`
	SupportsMatchURL  bool                  `json:"supports_match_url"`
	SupportsMatchText bool                  `json:"supports_match_text"`
}

type scriptPlugin struct {
	name   string
	interp *interp.Interpreter
	logger *logpkg.Logger
	mu     sync.Mutex
}

func newScriptPlugin(name string, interpreter *interp.Interpreter, logger *logpkg.Logger) *scriptPlugin {
	return &scriptPlugin{name: name, interp: interpreter, logger: logger}
}

func (p *scriptPlugin) Init(ctx context.Context, cfg map[string]string) error {
	fn, ok := p.lookup("Init")
	if !ok {
		return nil
	}
	_, err := p.call(ctx, fn, "init", "", cfg)
	return err
}

func (p *scriptPlugin) Meta(ctx context.Context) (*pluginMeta, error) {
	fn, ok := p.lookup("Meta")
	if !ok {
		return nil, fmt.Errorf("script plugin %s missing Meta", p.name)
	}
	result, err := p.call(ctx, fn, "meta", "")
	if err != nil {
		return nil, err
	}
	meta := &pluginMeta{}
	if err := decodeJSON(result, meta); err != nil {
		return nil, err
	}
	return meta, nil
}

func (p *scriptPlugin) MatchURL(ctx context.Context, platformName, rawURL string) (string, bool) {
	fn, ok := p.lookup("MatchURL")
	if !ok {
		return "", false
	}
	result, err := p.call(ctx, fn, "match", "", platformName, rawURL)
	if err != nil {
		return "", false
	}
	var resp struct {
		ID      string `json:"id"`
		Matched bool   `json:"matched"`
	}
	if err := decodeJSON(result, &resp); err != nil {
		return "", false
	}
	return resp.ID, resp.Matched
}

func (p *scriptPlugin) MatchText(ctx context.Context, platformName, text string) (string, bool) {
	fn, ok := p.lookup("MatchText")
	if !ok {
		return "", false
	}
	result, err := p.call(ctx, fn, "match", "", platformName, text)
	if err != nil {
		return "", false
	}
	var resp struct {
		ID      string `json:"id"`
		Matched bool   `json:"matched"`
	}
	if err := decodeJSON(result, &resp); err != nil {
		return "", false
	}
	return resp.ID, resp.Matched
}

func (p *scriptPlugin) Search(ctx context.Context, platformName, query string, limit int) ([]platform.Track, error) {
	fn, ok := p.lookup("Search")
	if !ok {
		return nil, platform.NewUnsupportedError(platformName, "search")
	}
	result, err := p.call(ctx, fn, "search", "", platformName, query, limit)
	if err != nil {
		return nil, err
	}
	var tracks []platform.Track
	if err := decodeJSON(result, &tracks); err != nil {
		return nil, err
	}
	return tracks, nil
}

func (p *scriptPlugin) GetTrack(ctx context.Context, platformName, trackID string) (*platform.Track, error) {
	fn, ok := p.lookup("GetTrack")
	if !ok {
		return nil, platform.NewUnsupportedError(platformName, "track")
	}
	result, err := p.call(ctx, fn, "track", trackID, platformName, trackID)
	if err != nil {
		return nil, err
	}
	var track platform.Track
	if err := decodeJSON(result, &track); err != nil {
		return nil, err
	}
	return &track, nil
}

func (p *scriptPlugin) GetLyrics(ctx context.Context, platformName, trackID string) (*platform.Lyrics, error) {
	fn, ok := p.lookup("GetLyrics")
	if !ok {
		return nil, platform.NewUnsupportedError(platformName, "lyrics")
	}
	result, err := p.call(ctx, fn, "lyrics", trackID, platformName, trackID)
	if err != nil {
		return nil, err
	}
	var lyrics platform.Lyrics
	if err := decodeJSON(result, &lyrics); err != nil {
		return nil, err
	}
	return &lyrics, nil
}

func (p *scriptPlugin) GetDownloadInfo(ctx context.Context, platformName, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	fn, ok := p.lookup("GetDownloadInfo")
	if !ok {
		return nil, platform.NewUnsupportedError(platformName, "download")
	}
	result, err := p.call(ctx, fn, "track", trackID, platformName, trackID, quality.String())
	if err != nil {
		return nil, err
	}
	return decodeDownloadInfo(result, platformName, trackID)
}

func (p *scriptPlugin) GetPlaylist(ctx context.Context, platformName, playlistID string) (*platform.Playlist, error) {
	fn, ok := p.lookup("GetPlaylist")
	if !ok {
		return nil, platform.NewUnsupportedError(platformName, "playlist")
	}
	result, err := p.call(ctx, fn, "playlist", playlistID, platformName, playlistID)
	if err != nil {
		return nil, err
	}
	var playlist platform.Playlist
	if err := decodeJSON(result, &playlist); err != nil {
		return nil, err
	}
	return &playlist, nil
}

func (p *scriptPlugin) lookup(name string) (reflect.Value, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	value, err := p.interp.Eval(fmt.Sprintf("%s.%s", p.name, name))
	if err != nil {
		return reflect.Value{}, false
	}
	return value, value.IsValid()
}

func (p *scriptPlugin) call(ctx context.Context, fn reflect.Value, resource, id string, args ...interface{}) (interface{}, error) {
	if !fn.IsValid() {
		return nil, fmt.Errorf("script function missing")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	inputs := make([]reflect.Value, 0, len(args))
	for _, arg := range args {
		inputs = append(inputs, reflect.ValueOf(arg))
	}
	p.mu.Lock()
	outputs := fn.Call(inputs)
	p.mu.Unlock()
	if len(outputs) == 0 {
		return nil, nil
	}
	if len(outputs) == 1 {
		return outputs[0].Interface(), nil
	}
	result := outputs[0].Interface()
	if err := asError(outputs[1]); err != nil {
		return nil, mapScriptError(err, resource, id, args)
	}
	return result, nil
}

func asError(value reflect.Value) error {
	if !value.IsValid() || value.IsNil() {
		return nil
	}
	if err, ok := value.Interface().(error); ok {
		return err
	}
	return fmt.Errorf("script error")
}

func mapScriptError(err error, resource, id string, args []interface{}) error {
	if err == nil {
		return nil
	}
	code := ""
	if coder, ok := err.(interface{ Code() string }); ok {
		code = strings.ToLower(strings.TrimSpace(coder.Code()))
	}
	plat := ""
	if len(args) > 0 {
		if v, ok := args[0].(string); ok {
			plat = v
		}
	}
	if resource == "" {
		resource = "track"
	}
	switch code {
	case "not_found":
		return platform.NewNotFoundError(plat, resource, id)
	case "unavailable":
		return platform.NewUnavailableError(plat, resource, id)
	case "unsupported":
		return platform.NewUnsupportedError(plat, resource)
	case "rate_limited":
		return platform.NewRateLimitedError(plat)
	case "auth_required":
		return platform.NewAuthRequiredError(plat)
	case "invalid":
		return fmt.Errorf("script invalid request: %s", err.Error())
	default:
		return err
	}
}

func decodeJSON(value interface{}, out interface{}) error {
	if value == nil {
		return fmt.Errorf("script returned empty")
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

type downloadInfoPayload struct {
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers,omitempty"`
	Size      int64             `json:"size"`
	Format    string            `json:"format"`
	Bitrate   int               `json:"bitrate"`
	MD5       string            `json:"md5,omitempty"`
	Quality   string            `json:"quality"`
	ExpiresAt *time.Time        `json:"expires_at,omitempty"`
}

func decodeDownloadInfo(value interface{}, platformName, trackID string) (*platform.DownloadInfo, error) {
	var payload downloadInfoPayload
	if err := decodeJSON(value, &payload); err != nil {
		return nil, err
	}
	quality := platform.QualityStandard
	if payload.Quality != "" {
		if q, err := platform.ParseQuality(payload.Quality); err == nil {
			quality = q
		}
	}
	if strings.TrimSpace(payload.URL) == "" {
		return nil, platform.NewUnavailableError(platformName, "track", trackID)
	}
	return &platform.DownloadInfo{
		URL:       payload.URL,
		Headers:   payload.Headers,
		Size:      payload.Size,
		Format:    payload.Format,
		Bitrate:   payload.Bitrate,
		MD5:       payload.MD5,
		Quality:   quality,
		ExpiresAt: payload.ExpiresAt,
	}, nil
}

type scriptPlatform struct {
	mu       sync.RWMutex
	plug     *scriptPlugin
	info     platformInfo
	name     string
	disabled bool
}

func newScriptPlatform(plugin *scriptPlugin, info platformInfo) *scriptPlatform {
	return &scriptPlatform{plug: plugin, info: info, name: info.Name}
}

func (s *scriptPlatform) Name() string { return s.name }

func (s *scriptPlatform) SupportsDownload() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.disabled && s.info.Capabilities.Download
}

func (s *scriptPlatform) SupportsSearch() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.disabled && s.info.Capabilities.Search
}

func (s *scriptPlatform) SupportsLyrics() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.disabled && s.info.Capabilities.Lyrics
}

func (s *scriptPlatform) SupportsRecognition() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.disabled && s.info.Capabilities.Recognition
}

func (s *scriptPlatform) Capabilities() platform.Capabilities {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.disabled {
		return platform.Capabilities{}
	}
	return s.info.Capabilities
}

func (s *scriptPlatform) GetDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	s.mu.RLock()
	plug := s.plug
	name := s.name
	disabled := s.disabled
	s.mu.RUnlock()
	if disabled || plug == nil {
		return nil, platform.NewUnsupportedError(name, "download")
	}
	return plug.GetDownloadInfo(ctx, name, trackID, quality)
}

func (s *scriptPlatform) Search(ctx context.Context, query string, limit int) ([]platform.Track, error) {
	s.mu.RLock()
	plug := s.plug
	name := s.name
	disabled := s.disabled
	s.mu.RUnlock()
	if disabled || plug == nil {
		return nil, platform.NewUnsupportedError(name, "search")
	}
	return plug.Search(ctx, name, query, limit)
}

func (s *scriptPlatform) GetLyrics(ctx context.Context, trackID string) (*platform.Lyrics, error) {
	s.mu.RLock()
	plug := s.plug
	name := s.name
	disabled := s.disabled
	s.mu.RUnlock()
	if disabled || plug == nil {
		return nil, platform.NewUnsupportedError(name, "lyrics")
	}
	return plug.GetLyrics(ctx, name, trackID)
}

func (s *scriptPlatform) RecognizeAudio(ctx context.Context, audioData io.Reader) (*platform.Track, error) {
	return nil, platform.NewUnsupportedError(s.name, "audio recognition")
}

func (s *scriptPlatform) GetTrack(ctx context.Context, trackID string) (*platform.Track, error) {
	s.mu.RLock()
	plug := s.plug
	name := s.name
	disabled := s.disabled
	s.mu.RUnlock()
	if disabled || plug == nil {
		return nil, platform.NewUnsupportedError(name, "track")
	}
	return plug.GetTrack(ctx, name, trackID)
}

func (s *scriptPlatform) GetArtist(ctx context.Context, artistID string) (*platform.Artist, error) {
	return nil, platform.NewUnsupportedError(s.name, "get artist")
}

func (s *scriptPlatform) GetAlbum(ctx context.Context, albumID string) (*platform.Album, error) {
	return nil, platform.NewUnsupportedError(s.name, "get album")
}

func (s *scriptPlatform) GetPlaylist(ctx context.Context, playlistID string) (*platform.Playlist, error) {
	s.mu.RLock()
	plug := s.plug
	name := s.name
	disabled := s.disabled
	s.mu.RUnlock()
	if disabled || plug == nil {
		return nil, platform.NewUnsupportedError(name, "playlist")
	}
	return plug.GetPlaylist(ctx, name, playlistID)
}

func (s *scriptPlatform) MatchURL(rawURL string) (trackID string, matched bool) {
	s.mu.RLock()
	plug := s.plug
	info := s.info
	name := s.name
	disabled := s.disabled
	s.mu.RUnlock()
	if disabled || plug == nil || !info.SupportsMatchURL {
		return "", false
	}
	return plug.MatchURL(context.Background(), name, rawURL)
}

func (s *scriptPlatform) MatchText(text string) (trackID string, matched bool) {
	s.mu.RLock()
	plug := s.plug
	info := s.info
	name := s.name
	disabled := s.disabled
	s.mu.RUnlock()
	if disabled || plug == nil || !info.SupportsMatchText {
		return "", false
	}
	return plug.MatchText(context.Background(), name, text)
}

func (s *scriptPlatform) update(plug *scriptPlugin, info platformInfo) {
	s.mu.Lock()
	s.plug = plug
	s.info = info
	s.name = info.Name
	s.disabled = false
	s.mu.Unlock()
}

func (s *scriptPlatform) disable() {
	s.mu.Lock()
	s.disabled = true
	s.plug = nil
	s.mu.Unlock()
}
