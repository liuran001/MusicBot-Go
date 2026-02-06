package handler

import (
	"context"
	"io"
	"sync"
	"time"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// stubSongRepository implements botpkg.SongRepository with in-memory maps for testing.
type stubSongRepository struct {
	mu            sync.RWMutex
	songs         map[int]*botpkg.SongInfo        // by MusicID
	platformSongs map[string]*botpkg.SongInfo     // by "platform:trackID:quality"
	fileSongs     map[string]*botpkg.SongInfo     // by FileID
	userSettings  map[int64]*botpkg.UserSettings  // by UserID
	groupSettings map[int64]*botpkg.GroupSettings // by ChatID
	sendCount     int64
}

func newStubRepo() *stubSongRepository {
	return &stubSongRepository{
		songs:         make(map[int]*botpkg.SongInfo),
		platformSongs: make(map[string]*botpkg.SongInfo),
		fileSongs:     make(map[string]*botpkg.SongInfo),
		userSettings:  make(map[int64]*botpkg.UserSettings),
		groupSettings: make(map[int64]*botpkg.GroupSettings),
	}
}

func (r *stubSongRepository) FindByMusicID(ctx context.Context, musicID int) (*botpkg.SongInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if song, ok := r.songs[musicID]; ok {
		return song, nil
	}
	return nil, nil
}

func (r *stubSongRepository) FindByPlatformTrackID(ctx context.Context, platform, trackID, quality string) (*botpkg.SongInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key := platform + ":" + trackID + ":" + quality
	if song, ok := r.platformSongs[key]; ok {
		return song, nil
	}
	return nil, nil
}

func (r *stubSongRepository) FindByFileID(ctx context.Context, fileID string) (*botpkg.SongInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if song, ok := r.fileSongs[fileID]; ok {
		return song, nil
	}
	return nil, nil
}

func (r *stubSongRepository) Create(ctx context.Context, song *botpkg.SongInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if song.MusicID != 0 {
		r.songs[song.MusicID] = song
	}
	if song.Platform != "" && song.TrackID != "" && song.Quality != "" {
		key := song.Platform + ":" + song.TrackID + ":" + song.Quality
		r.platformSongs[key] = song
	}
	if song.FileID != "" {
		r.fileSongs[song.FileID] = song
	}
	return nil
}

func (r *stubSongRepository) Update(ctx context.Context, song *botpkg.SongInfo) error {
	return r.Create(ctx, song)
}

func (r *stubSongRepository) Delete(ctx context.Context, musicID int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.songs, musicID)
	return nil
}

func (r *stubSongRepository) DeleteAll(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.songs = make(map[int]*botpkg.SongInfo)
	r.platformSongs = make(map[string]*botpkg.SongInfo)
	r.fileSongs = make(map[string]*botpkg.SongInfo)
	return nil
}

func (r *stubSongRepository) DeleteAllByPlatform(ctx context.Context, platform string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, song := range r.platformSongs {
		if song != nil && song.Platform == platform {
			delete(r.platformSongs, key)
		}
	}
	return nil
}

func (r *stubSongRepository) DeleteByPlatformTrackID(ctx context.Context, platform, trackID, quality string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := platform + ":" + trackID + ":" + quality
	delete(r.platformSongs, key)
	return nil
}

func (r *stubSongRepository) DeleteAllQualitiesByPlatformTrackID(ctx context.Context, platform, trackID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, song := range r.platformSongs {
		if song != nil && song.Platform == platform && song.TrackID == trackID {
			delete(r.platformSongs, key)
		}
	}
	return nil
}

func (r *stubSongRepository) Count(ctx context.Context) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return int64(len(r.songs)), nil
}

func (r *stubSongRepository) CountByUserID(ctx context.Context, userID int64) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := int64(0)
	for _, song := range r.songs {
		if song.FromUserID == userID {
			count++
		}
	}
	return count, nil
}

func (r *stubSongRepository) CountByChatID(ctx context.Context, chatID int64) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := int64(0)
	for _, song := range r.songs {
		if song.FromChatID == chatID {
			count++
		}
	}
	return count, nil
}

func (r *stubSongRepository) CountByPlatform(ctx context.Context) (map[string]int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]int64)
	for _, song := range r.songs {
		result[song.Platform]++
	}
	return result, nil
}

func (r *stubSongRepository) GetSendCount(ctx context.Context) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sendCount, nil
}

func (r *stubSongRepository) IncrementSendCount(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sendCount++
	return nil
}

func (r *stubSongRepository) Last(ctx context.Context) (*botpkg.SongInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var latest *botpkg.SongInfo
	for _, song := range r.songs {
		if latest == nil || song.UpdatedAt.After(latest.UpdatedAt) {
			latest = song
		}
	}
	return latest, nil
}

func (r *stubSongRepository) GetUserSettings(ctx context.Context, userID int64) (*botpkg.UserSettings, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if settings, ok := r.userSettings[userID]; ok {
		return settings, nil
	}
	return nil, nil
}

func (r *stubSongRepository) UpdateUserSettings(ctx context.Context, settings *botpkg.UserSettings) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.userSettings[settings.UserID] = settings
	return nil
}

func (r *stubSongRepository) GetGroupSettings(ctx context.Context, chatID int64) (*botpkg.GroupSettings, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if settings, ok := r.groupSettings[chatID]; ok {
		return settings, nil
	}
	return nil, nil
}

func (r *stubSongRepository) UpdateGroupSettings(ctx context.Context, settings *botpkg.GroupSettings) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.groupSettings[settings.ChatID] = settings
	return nil
}

// stubPlatformManager implements platform.Manager for testing.
type stubPlatformManager struct {
	mu        sync.RWMutex
	platforms map[string]platform.Platform
	urlRules  map[string]urlRule
	textRules map[string]textRule
	aliases   map[string]string
	metas     map[string]platform.Meta
}

type urlRule struct {
	platformName string
	trackID      string
}

type textRule struct {
	platformName string
	trackID      string
}

func newStubManager() *stubPlatformManager {
	return &stubPlatformManager{
		platforms: make(map[string]platform.Platform),
		urlRules:  make(map[string]urlRule),
		textRules: make(map[string]textRule),
		aliases:   make(map[string]string),
		metas:     make(map[string]platform.Meta),
	}
}

func (m *stubPlatformManager) Register(plat platform.Platform) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.platforms[plat.Name()] = plat
	if metaProvider, ok := plat.(platform.MetadataProvider); ok {
		meta := metaProvider.Metadata()
		if meta.Name == "" {
			meta.Name = plat.Name()
		}
		m.metas[meta.Name] = meta
		for _, alias := range meta.Aliases {
			aliasKey := platform.NormalizeAliasToken(alias)
			if aliasKey == "" {
				continue
			}
			if _, exists := m.aliases[aliasKey]; !exists {
				m.aliases[aliasKey] = meta.Name
			}
		}
	}
}

func (m *stubPlatformManager) Get(name string) platform.Platform {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.platforms[name]
}

func (m *stubPlatformManager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.platforms))
	for name := range m.platforms {
		names = append(names, name)
	}
	return names
}

func (m *stubPlatformManager) MatchURL(url string) (platformName, trackID string, matched bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if rule, ok := m.urlRules[url]; ok {
		return rule.platformName, rule.trackID, true
	}
	return "", "", false
}

func (m *stubPlatformManager) MatchText(text string) (platformName, trackID string, matched bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if rule, ok := m.textRules[text]; ok {
		return rule.platformName, rule.trackID, true
	}
	return "", "", false
}

func (m *stubPlatformManager) ResolveAlias(alias string) (string, bool) {
	key := platform.NormalizeAliasToken(alias)
	if key == "" {
		return "", false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.platforms[key]; ok {
		return key, true
	}
	if name, ok := m.aliases[key]; ok {
		return name, true
	}
	return "", false
}

func (m *stubPlatformManager) Meta(name string) (platform.Meta, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	meta, ok := m.metas[name]
	if !ok {
		return platform.Meta{Name: name, DisplayName: name, Emoji: "🎵"}, false
	}
	return meta, true
}

func (m *stubPlatformManager) ListMeta() []platform.Meta {
	names := m.List()
	metas := make([]platform.Meta, 0, len(names))
	for _, name := range names {
		meta, _ := m.Meta(name)
		metas = append(metas, meta)
	}
	return metas
}

func (m *stubPlatformManager) AddURLRule(url, platformName, trackID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.urlRules[url] = urlRule{platformName: platformName, trackID: trackID}
}

func (m *stubPlatformManager) AddTextRule(text, platformName, trackID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.textRules[text] = textRule{platformName: platformName, trackID: trackID}
}

// stubPlatform is a minimal platform.Platform implementation for testing.
type stubPlatform struct {
	name string
}

func newStubPlatform(name string) *stubPlatform {
	return &stubPlatform{name: name}
}

func (p *stubPlatform) Name() string {
	return p.name
}

func (p *stubPlatform) SupportsDownload() bool {
	return true
}

func (p *stubPlatform) SupportsSearch() bool {
	return true
}

func (p *stubPlatform) SupportsLyrics() bool {
	return true
}

func (p *stubPlatform) SupportsRecognition() bool {
	return false
}

func (p *stubPlatform) Capabilities() platform.Capabilities {
	return platform.Capabilities{}
}

func (p *stubPlatform) GetDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	return nil, platform.ErrUnsupported
}

func (p *stubPlatform) Search(ctx context.Context, query string, limit int) ([]platform.Track, error) {
	return nil, platform.ErrUnsupported
}

func (p *stubPlatform) GetLyrics(ctx context.Context, trackID string) (*platform.Lyrics, error) {
	return nil, platform.ErrUnsupported
}

func (p *stubPlatform) RecognizeAudio(ctx context.Context, audioData io.Reader) (*platform.Track, error) {
	return nil, platform.ErrUnsupported
}

func (p *stubPlatform) GetTrack(ctx context.Context, trackID string) (*platform.Track, error) {
	return &platform.Track{
		ID:       trackID,
		Title:    "Test Track",
		Duration: 180 * time.Second,
		Artists: []platform.Artist{
			{ID: "artist1", Name: "Test Artist"},
		},
		Album: &platform.Album{
			ID:    "album1",
			Title: "Test Album",
		},
	}, nil
}

func (p *stubPlatform) GetArtist(ctx context.Context, artistID string) (*platform.Artist, error) {
	return nil, platform.ErrUnsupported
}

func (p *stubPlatform) GetAlbum(ctx context.Context, albumID string) (*platform.Album, error) {
	return nil, platform.ErrUnsupported
}

func (p *stubPlatform) GetPlaylist(ctx context.Context, playlistID string) (*platform.Playlist, error) {
	return nil, platform.ErrUnsupported
}
