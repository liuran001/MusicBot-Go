package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/db"
	"github.com/liuran001/MusicBot-Go/bot/download"
	"github.com/liuran001/MusicBot-Go/bot/i18n"
	"github.com/liuran001/MusicBot-Go/bot/id3"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/mymmrac/telego"
	"go.senan.xyz/taglib"
	"gorm.io/gorm/logger"
)

type localizedFlowPlatform struct {
	*stubPlatform
	payload           []byte
	tracks, downloads atomic.Int32
}

func (p *localizedFlowPlatform) LocalizeTrack(_ context.Context, source *platform.Track) (*platform.Track, error) {
	out := *source
	out.Title = "中文歌名"
	out.Artists = []platform.Artist{{Name: "中文歌手"}}
	out.Album = &platform.Album{Title: "中文专辑"}
	out.MetadataLanguage = "zh"
	return &out, nil
}
func (p *localizedFlowPlatform) GetTrack(ctx context.Context, id string) (*platform.Track, error) {
	p.tracks.Add(1)
	return p.LocalizeTrack(ctx, &platform.Track{ID: id, Platform: "applemusic", Duration: time.Second})
}
func (p *localizedFlowPlatform) GetDownloadInfo(context.Context, string, platform.Quality) (*platform.DownloadInfo, error) {
	return &platform.DownloadInfo{URL: "test://fresh-audio", Format: "m4a", Quality: platform.QualityHigh, Downloader: func(_ context.Context, _ *platform.DownloadInfo, path string, _ func(int64, int64)) (int64, error) {
		p.downloads.Add(1)
		return int64(len(p.payload)), os.WriteFile(path, p.payload, 0600)
	}}, nil
}

func TestLocalizedAudioRetrievalFailureRedownloadsAndCachesNewLanguage(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg required")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe required")
	}
	dir := t.TempDir()
	fixture := filepath.Join(dir, "source.m4a")
	if output, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-c:a", "aac", fixture).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, output)
	}
	payload, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := db.NewSQLiteRepository(filepath.Join(dir, "cache.db"), filepath.Join(dir, "data.db"), logger.Default.LogMode(logger.Silent))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	source := &botpkg.SongInfo{Platform: "applemusic", TrackID: "id", Quality: "high", FileExt: "m4a", FileID: "old-en-file", Duration: 1, AudioValidated: true, MetadataLanguage: "en", AudioLanguage: "en", SongName: "Old title", SongArtists: "Old artist", SongAlbum: "Old album"}
	if err := repo.Create(zhCtx(), source); err != nil {
		t.Fatal(err)
	}
	var getFiles, uploads atomic.Int32
	uploadedTagResults := make(chan map[string][]string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/getFile"):
			getFiles.Add(1)
			io.WriteString(w, `{"ok":false,"error_code":400,"description":"file unavailable"}`)
		case strings.HasSuffix(r.URL.Path, "/sendAudio"):
			uploads.Add(1)
			if err := r.ParseMultipartForm(4 << 20); err != nil {
				t.Errorf("upload body: %v", err)
				http.Error(w, "bad upload", 400)
				return
			}
			defer r.MultipartForm.RemoveAll()
			file, _, err := r.FormFile("audio")
			if err != nil {
				t.Errorf("audio not uploaded: %v", err)
				http.Error(w, "bad upload", 400)
				return
			}
			defer file.Close()
			data, err := io.ReadAll(file)
			if err != nil {
				t.Error(err)
			}
			out := filepath.Join(dir, "uploaded.m4a")
			if err := os.WriteFile(out, data, 0600); err != nil {
				t.Error(err)
			}
			uploadedTags, err := taglib.ReadTags(out)
			if err != nil {
				t.Error(err)
			}
			uploadedTagResults <- uploadedTags
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 1, "date": 1, "chat": map[string]any{"id": 1001, "type": "private"}, "audio": map[string]any{"file_id": "new-zh-file", "file_unique_id": "unique", "duration": 1}}})
		default:
			t.Errorf("unexpected Telegram request: %s", r.URL.Path)
			http.Error(w, "unexpected", 400)
		}
	}))
	defer server.Close()
	b, err := telego.NewBot("123456:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghi", telego.WithAPIServer(server.URL), telego.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	p := &localizedFlowPlatform{stubPlatform: newStubPlatform("applemusic"), payload: payload}
	manager := newStubManager()
	manager.Register(p)
	h := &MusicHandler{Repo: repo, PlatformManager: manager, DownloadService: download.NewDownloadService(download.DownloadServiceOptions{}), ID3Service: id3.NewID3Service(nil), CacheDir: filepath.Join(dir, "files"), DefaultQuality: "high", InlineUploadChatID: 1001}
	if err := os.MkdirAll(h.CacheDir, 0700); err != nil {
		t.Fatal(err)
	}
	got, err := h.prepareInlineSong(zhCtx(), b, 1, 0, false, "user", "applemusic", "id", "high", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.FileID != "new-zh-file" || got.AudioLanguage != "zh" || got.MetadataLanguage != "zh" {
		t.Fatalf("wrong new audio: %+v", got)
	}
	uploadedTags := <-uploadedTagResults
	if len(uploadedTags[taglib.Title]) != 1 || uploadedTags[taglib.Title][0] != "中文歌名" {
		t.Fatalf("upload tags: %v", uploadedTags)
	}
	if getFiles.Load() != 1 || p.downloads.Load() != 1 || uploads.Load() != 1 {
		t.Fatalf("getFile=%d downloads=%d uploads=%d", getFiles.Load(), p.downloads.Load(), uploads.Load())
	}
	old, err := repo.FindLocalizedAudio(zhCtx(), "applemusic", "id", "high", "en")
	if err != nil || old == nil || old.FileID != "old-en-file" {
		t.Fatalf("original lost: %+v %v", old, err)
	}
	again, err := h.prepareInlineSong(zhCtx(), b, 1, 0, false, "user", "applemusic", "id", "high", nil, nil)
	if err != nil || again.FileID != got.FileID || p.downloads.Load() != 1 || uploads.Load() != 1 || getFiles.Load() != 1 {
		t.Fatalf("did not reuse target language: %+v %v", again, err)
	}
	if err := repo.DeleteLocalizedAudio(zhCtx(), "applemusic", "id", "high", "zh"); err != nil {
		t.Fatal(err)
	}
	remaining, err := findLanguageAudio(zhCtx(), repo, "applemusic", "id", "high")
	if err != nil || remaining == nil || remaining.FileID != "old-en-file" {
		t.Fatalf("other-language source lost after invalidation: %+v %v", remaining, err)
	}
	for _, validated := range []bool{false, true} {
		incomplete := *got
		incomplete.ID = 0
		incomplete.FileID = ""
		incomplete.AudioValidated = validated
		if err := repo.Create(zhCtx(), &incomplete); err != nil {
			t.Fatal(err)
		}
		remaining, err := findLanguageAudio(zhCtx(), repo, "applemusic", "id", "high")
		if err != nil || remaining == nil || remaining.FileID != "old-en-file" {
			t.Fatalf("incomplete primary hides retained source (validated=%v): %+v %v", validated, remaining, err)
		}
	}
}

type unchangedMetadataPlatform struct {
	*stubPlatform
	fail bool
}

func (p *unchangedMetadataPlatform) LocalizeTrack(ctx context.Context, track *platform.Track) (*platform.Track, error) {
	out := *track
	if !p.fail {
		out.MetadataLanguage = i18n.From(ctx).Lang()
	}
	return &out, nil
}

func TestUnchangedCachedNamesMarkLanguageWithoutMediaTransfer(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint("lookupFallback=", failed), func(t *testing.T) {
			dir := t.TempDir()
			repo, err := db.NewSQLiteRepository(filepath.Join(dir, "cache.db"), filepath.Join(dir, "data.db"), logger.Default.LogMode(logger.Silent))
			if err != nil {
				t.Fatal(err)
			}
			defer repo.Close()
			ctx := i18n.WithLocalizer(context.Background(), i18n.For("en"))
			source := &botpkg.SongInfo{Platform: "applemusic", TrackID: "same", Quality: "high", FileID: "original-file", AudioValidated: true, SongName: "English title", SongArtists: "English artist", SongAlbum: "English album"}
			if err := repo.Create(ctx, source); err != nil {
				t.Fatal(err)
			}
			manager := newStubManager()
			manager.Register(&unchangedMetadataPlatform{stubPlatform: newStubPlatform("applemusic"), fail: failed})
			h := &MusicHandler{Repo: repo, PlatformManager: manager, InlineUploadChatID: 1}
			// No bot or media services: any retrieval/upload attempt would fail.
			got, err := h.tryLocalizedInlineAudio(ctx, nil, "applemusic", "same", "high", nil)
			if err != nil || got == nil || got.FileID != source.FileID {
				t.Fatalf("reuse: %+v %v", got, err)
			}
			want := "en"
			if failed {
				want = ""
			}
			if got.AudioLanguage != want {
				t.Fatalf("language=%q want %q", got.AudioLanguage, want)
			}
			stored, err := repo.FindLocalizedAudio(ctx, "applemusic", "same", "high", "en")
			if err != nil {
				t.Fatal(err)
			}
			if failed {
				if stored != nil {
					t.Fatal("fallback incorrectly marked as localized")
				}
			} else if stored == nil || stored.FileID != source.FileID {
				t.Fatalf("language marker not persisted: %+v", stored)
			}
			invalidateLanguageAudio(ctx, repo, got)
			if remaining, _ := findLanguageAudio(ctx, repo, "applemusic", "same", "high"); remaining != nil {
				t.Fatalf("invalid FileID resurrected from another alias: %+v", remaining)
			}
			// Invalidating an old file must not remove a newer language version.
			other := *source
			other.ID = 0
			other.FileID = "different-file"
			other.AudioLanguage = "zh"
			if err := repo.SaveLocalizedAudio(ctx, &other); err != nil {
				t.Fatal(err)
			}
			invalidateLanguageAudio(ctx, repo, got)
			if remaining, _ := findLanguageAudio(ctx, repo, "applemusic", "same", "high"); remaining == nil || remaining.FileID != other.FileID {
				t.Fatalf("different file lost: %+v", remaining)
			}
		})
	}
}
