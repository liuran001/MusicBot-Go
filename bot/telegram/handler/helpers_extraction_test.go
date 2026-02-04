package handler

import (
	"context"
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/mymmrac/telego"
)

func TestCommandArguments(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "command with args",
			input: "/music netease 123 hires",
			want:  "netease 123 hires",
		},
		{
			name:  "command with single arg",
			input: "/search test",
			want:  "test",
		},
		{
			name:  "command no args",
			input: "/help",
			want:  "",
		},
		{
			name:  "not a command",
			input: "just text",
			want:  "",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "command with whitespace",
			input: "/music   netease 123",
			want:  "netease 123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := commandArguments(tt.input)
			if got != tt.want {
				t.Errorf("commandArguments(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractPlatformTrack_CommandArgs(t *testing.T) {
	mgr := newStubManager()
	mgr.Register(newStubPlatform("netease"))

	tests := []struct {
		name         string
		text         string
		wantPlatform string
		wantTrackID  string
		wantFound    bool
	}{
		{
			name:         "command args with quality",
			text:         "/music netease 12345 hires",
			wantPlatform: "netease",
			wantTrackID:  "12345",
			wantFound:    true,
		},
		{
			name:         "command args with standard quality",
			text:         "/music spotify abc123 standard",
			wantPlatform: "spotify",
			wantTrackID:  "abc123",
			wantFound:    true,
		},
		{
			name:         "command args no quality",
			text:         "/music netease 12345",
			wantPlatform: "",
			wantTrackID:  "",
			wantFound:    false,
		},
		{
			name:         "command args invalid quality",
			text:         "/music netease 12345 invalid",
			wantPlatform: "",
			wantTrackID:  "",
			wantFound:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &telego.Message{Text: tt.text}
			gotPlatform, gotTrackID, gotFound := extractPlatformTrack(context.Background(), msg, mgr)
			if gotPlatform != tt.wantPlatform || gotTrackID != tt.wantTrackID || gotFound != tt.wantFound {
				t.Errorf("extractPlatformTrack() = (%q, %q, %v), want (%q, %q, %v)",
					gotPlatform, gotTrackID, gotFound, tt.wantPlatform, tt.wantTrackID, tt.wantFound)
			}
		})
	}
}

func TestExtractPlatformTrack_MatchText(t *testing.T) {
	mgr := newStubManager()
	mgr.AddTextRule("netease:12345", "netease", "12345")
	mgr.AddTextRule("12345", "netease", "12345")

	tests := []struct {
		name         string
		text         string
		wantPlatform string
		wantTrackID  string
		wantFound    bool
	}{
		{
			name:         "text match",
			text:         "netease:12345",
			wantPlatform: "netease",
			wantTrackID:  "12345",
			wantFound:    true,
		},
		{
			name:         "numeric text match",
			text:         "12345",
			wantPlatform: "netease",
			wantTrackID:  "12345",
			wantFound:    true,
		},
		{
			name:         "no match",
			text:         "unknown",
			wantPlatform: "",
			wantTrackID:  "",
			wantFound:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &telego.Message{Text: tt.text}
			gotPlatform, gotTrackID, gotFound := extractPlatformTrack(context.Background(), msg, mgr)
			if gotPlatform != tt.wantPlatform || gotTrackID != tt.wantTrackID || gotFound != tt.wantFound {
				t.Errorf("extractPlatformTrack() = (%q, %q, %v), want (%q, %q, %v)",
					gotPlatform, gotTrackID, gotFound, tt.wantPlatform, tt.wantTrackID, tt.wantFound)
			}
		})
	}
}

func TestExtractPlatformTrack_MatchURL(t *testing.T) {
	mgr := newStubManager()
	mgr.AddURLRule("https://music.163.com/song?id=12345", "netease", "12345")
	mgr.AddURLRule("https://open.spotify.com/track/abc123", "spotify", "abc123")

	tests := []struct {
		name         string
		text         string
		wantPlatform string
		wantTrackID  string
		wantFound    bool
	}{
		{
			name:         "netease URL",
			text:         "https://music.163.com/song?id=12345",
			wantPlatform: "netease",
			wantTrackID:  "12345",
			wantFound:    true,
		},
		{
			name:         "spotify URL",
			text:         "https://open.spotify.com/track/abc123",
			wantPlatform: "spotify",
			wantTrackID:  "abc123",
			wantFound:    true,
		},
		{
			name:         "no URL match",
			text:         "https://example.com/unknown",
			wantPlatform: "",
			wantTrackID:  "",
			wantFound:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &telego.Message{Text: tt.text}
			gotPlatform, gotTrackID, gotFound := extractPlatformTrack(context.Background(), msg, mgr)
			if gotPlatform != tt.wantPlatform || gotTrackID != tt.wantTrackID || gotFound != tt.wantFound {
				t.Errorf("extractPlatformTrack() = (%q, %q, %v), want (%q, %q, %v)",
					gotPlatform, gotTrackID, gotFound, tt.wantPlatform, tt.wantTrackID, tt.wantFound)
			}
		})
	}
}

func TestExtractPlatformTrack_NilMessage(t *testing.T) {
	mgr := newStubManager()
	gotPlatform, gotTrackID, gotFound := extractPlatformTrack(context.Background(), nil, mgr)
	if gotPlatform != "" || gotTrackID != "" || gotFound != false {
		t.Errorf("extractPlatformTrack(nil) = (%q, %q, %v), want (\"\", \"\", false)",
			gotPlatform, gotTrackID, gotFound)
	}
}

func TestExtractPlatformTrack_EmptyText(t *testing.T) {
	mgr := newStubManager()
	msg := &telego.Message{Text: ""}
	gotPlatform, gotTrackID, gotFound := extractPlatformTrack(context.Background(), msg, mgr)
	if gotPlatform != "" || gotTrackID != "" || gotFound != false {
		t.Errorf("extractPlatformTrack(empty) = (%q, %q, %v), want (\"\", \"\", false)",
			gotPlatform, gotTrackID, gotFound)
	}
}

func TestExtractQualityOverride(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "valid quality hires",
			text: "/music netease 12345 hires",
			want: "hires",
		},
		{
			name: "valid quality lossless",
			text: "/music netease 12345 lossless",
			want: "lossless",
		},
		{
			name: "valid quality high",
			text: "/music netease 12345 high",
			want: "high",
		},
		{
			name: "valid quality standard",
			text: "/music netease 12345 standard",
			want: "standard",
		},
		{
			name: "invalid quality",
			text: "/music netease 12345 invalid",
			want: "",
		},
		{
			name: "no quality",
			text: "/music netease 12345",
			want: "",
		},
		{
			name: "no args",
			text: "/music",
			want: "",
		},
		{
			name: "not a command",
			text: "netease 12345 hires",
			want: "hires",
		},
		{
			name: "text quality high",
			text: "周杰伦 high",
			want: "high",
		},
		{
			name: "text quality low",
			text: "周杰伦 low",
			want: "standard",
		},
		{
			name: "empty text",
			text: "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &telego.Message{Text: tt.text}
			got := extractQualityOverride(msg, nil)
			if got != tt.want {
				t.Errorf("extractQualityOverride(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestExtractQualityOverride_NilMessage(t *testing.T) {
	got := extractQualityOverride(nil, nil)
	if got != "" {
		t.Errorf("extractQualityOverride(nil) = %q, want \"\"", got)
	}
}

func TestParseQuality_Integration(t *testing.T) {
	validQualities := []string{"hires", "lossless", "high", "standard"}
	for _, q := range validQualities {
		t.Run(q, func(t *testing.T) {
			_, err := platform.ParseQuality(q)
			if err != nil {
				t.Errorf("ParseQuality(%q) returned error: %v", q, err)
			}
		})
	}

	invalidQualities := []string{"invalid", "unknown", ""}
	for _, q := range invalidQualities {
		t.Run("invalid_"+q, func(t *testing.T) {
			_, err := platform.ParseQuality(q)
			if err == nil {
				t.Errorf("ParseQuality(%q) should return error", q)
			}
		})
	}
}
