package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

func TestExtractPlatformTrackFromMessage(t *testing.T) {
	mgr := newStubManager()
	mgr.AddTextRule("12345", "netease", "12345")
	mgr.AddURLRule("https://music.163.com/song?id=12345", "netease", "12345")

	tests := []struct {
		name         string
		messageText  string
		wantPlatform string
		wantTrackID  string
		wantFound    bool
	}{
		{
			name:         "text match",
			messageText:  "12345",
			wantPlatform: "netease",
			wantTrackID:  "12345",
			wantFound:    true,
		},
		{
			name:         "URL match",
			messageText:  "https://music.163.com/song?id=12345",
			wantPlatform: "netease",
			wantTrackID:  "12345",
			wantFound:    true,
		},
		{
			name:         "no match",
			messageText:  "unknown",
			wantPlatform: "",
			wantTrackID:  "",
			wantFound:    false,
		},
		{
			name:         "empty message",
			messageText:  "",
			wantPlatform: "",
			wantTrackID:  "",
			wantFound:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPlatform, gotTrackID, gotFound := extractPlatformTrackFromMessage(context.Background(), tt.messageText, mgr)
			if gotPlatform != tt.wantPlatform || gotTrackID != tt.wantTrackID || gotFound != tt.wantFound {
				t.Errorf("extractPlatformTrackFromMessage() = (%q, %q, %v), want (%q, %q, %v)",
					gotPlatform, gotTrackID, gotFound, tt.wantPlatform, tt.wantTrackID, tt.wantFound)
			}
		})
	}
}

func TestExtractPlatformTrackFromMessage_NilManager(t *testing.T) {
	gotPlatform, gotTrackID, gotFound := extractPlatformTrackFromMessage(context.Background(), "test", nil)
	if gotPlatform != "" || gotTrackID != "" || gotFound != false {
		t.Errorf("extractPlatformTrackFromMessage(nil manager) = (%q, %q, %v), want (\"\", \"\", false)",
			gotPlatform, gotTrackID, gotFound)
	}
}

func TestFormatLyricsError(t *testing.T) {
	handler := &LyricHandler{}

	tests := []struct {
		name    string
		err     error
		wantStr string
	}{
		{
			name:    "ErrNotFound",
			err:     platform.ErrNotFound,
			wantStr: "未找到歌曲或歌词",
		},
		{
			name:    "ErrUnavailable",
			err:     platform.ErrUnavailable,
			wantStr: "此歌曲无法获取歌词",
		},
		{
			name:    "ErrUnsupported",
			err:     platform.ErrUnsupported,
			wantStr: "此平台不支持获取歌词",
		},
		{
			name:    "other error",
			err:     errors.New("random error"),
			wantStr: getLrcFailed,
		},
		{
			name:    "nil error",
			err:     nil,
			wantStr: getLrcFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.formatLyricsError(tt.err)
			if got != tt.wantStr {
				t.Errorf("formatLyricsError(%v) = %q, want %q", tt.err, got, tt.wantStr)
			}
		})
	}
}

func TestFormatLyricsError_WrappedErrors(t *testing.T) {
	handler := &LyricHandler{}

	wrappedNotFound := errors.Join(platform.ErrNotFound, errors.New("detail"))
	got := handler.formatLyricsError(wrappedNotFound)
	if got != "未找到歌曲或歌词" {
		t.Errorf("formatLyricsError(wrapped ErrNotFound) = %q, want %q", got, "未找到歌曲或歌词")
	}

	wrappedUnavailable := errors.Join(platform.ErrUnavailable, errors.New("detail"))
	got = handler.formatLyricsError(wrappedUnavailable)
	if got != "此歌曲无法获取歌词" {
		t.Errorf("formatLyricsError(wrapped ErrUnavailable) = %q, want %q", got, "此歌曲无法获取歌词")
	}

	wrappedUnsupported := errors.Join(platform.ErrUnsupported, errors.New("detail"))
	got = handler.formatLyricsError(wrappedUnsupported)
	if got != "此平台不支持获取歌词" {
		t.Errorf("formatLyricsError(wrapped ErrUnsupported) = %q, want %q", got, "此平台不支持获取歌词")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{
			name:     "zero",
			duration: 0,
			want:     "00:00.00",
		},
		{
			name:     "30 seconds",
			duration: 30 * time.Second,
			want:     "00:30.00",
		},
		{
			name:     "1 minute",
			duration: 1 * time.Minute,
			want:     "01:00.00",
		},
		{
			name:     "3 minutes 45 seconds",
			duration: 3*time.Minute + 45*time.Second,
			want:     "03:45.00",
		},
		{
			name:     "10 minutes 5 seconds",
			duration: 10*time.Minute + 5*time.Second,
			want:     "10:05.00",
		},
		{
			name:     "59 minutes 59 seconds",
			duration: 59*time.Minute + 59*time.Second,
			want:     "59:59.00",
		},
		{
			name:     "over 1 hour",
			duration: 75 * time.Minute,
			want:     "75:00.00",
		},
		{
			name:     "with centiseconds",
			duration: 2*time.Minute + 3*time.Second + 450*time.Millisecond,
			want:     "02:03.45",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.duration)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.duration, got, tt.want)
			}
		})
	}
}
