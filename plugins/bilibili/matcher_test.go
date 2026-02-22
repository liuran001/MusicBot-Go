package bilibili

import (
	"testing"
)

func TestURLMatcher_MatchURL(t *testing.T) {
	matcher := NewURLMatcher()

	tests := []struct {
		name      string
		url       string
		wantId    string
		wantMatch bool
	}{
		{
			name:      "Standard audio URL",
			url:       "https://www.bilibili.com/audio/au123456",
			wantId:    "123456",
			wantMatch: true,
		},
		{
			name:      "Audio URL without protocol",
			url:       "bilibili.com/audio/au987654",
			wantId:    "987654",
			wantMatch: true,
		},
		{
			name:      "Valid standard BV Video URL",
			url:       "https://www.bilibili.com/video/BV1GJ411x7h7",
			wantId:    "BV1GJ411x7h7",
			wantMatch: true,
		},
		{
			name:      "Valid standard AV Video URL",
			url:       "https://www.bilibili.com/video/av170001",
			wantId:    "av170001",
			wantMatch: true,
		},
		{
			name:      "Valid short BV URL",
			url:       "b23.tv/BV1GJ411x7h7",
			wantId:    "BV1GJ411x7h7",
			wantMatch: true,
		},
		{
			name:      "Valid short AV URL",
			url:       "b23.tv/av170001",
			wantId:    "av170001",
			wantMatch: true,
		},
		{
			name:      "Valid b23 pure shorturl",
			url:       "https://b23.tv/ysjTEMn",
			wantId:    "b23:ysjTEMn",
			wantMatch: true,
		},
		{
			name:      "Non bilibili URL",
			url:       "https://music.163.com/#/song?id=123",
			wantId:    "",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotId, gotMatch := matcher.MatchURL(tt.url)
			if gotId != tt.wantId {
				t.Errorf("MatchURL() gotId = %v, want %v", gotId, tt.wantId)
			}
			if gotMatch != tt.wantMatch {
				t.Errorf("MatchURL() gotMatch = %v, want %v", gotMatch, tt.wantMatch)
			}
		})
	}
}

func TestURLMatcher_MatchText(t *testing.T) {
	matcher := NewURLMatcher()

	tests := []struct {
		name      string
		text      string
		wantId    string
		wantMatch bool
	}{
		{
			name:      "Valid audio text id",
			text:      "au123456",
			wantId:    "123456",
			wantMatch: true,
		},
		{
			name:      "Valid BV text id",
			text:      "BV1GJ411x7h7",
			wantId:    "BV1GJ411x7h7",
			wantMatch: true,
		},
		{
			name:      "Valid av text id",
			text:      "av170001",
			wantId:    "av170001",
			wantMatch: true,
		},
		{
			name:      "Valid AV text id uppercase",
			text:      "AV170001",
			wantId:    "AV170001",
			wantMatch: true,
		},
		{
			name:      "Invalid text id missing au",
			text:      "123456",
			wantId:    "",
			wantMatch: false,
		},
		{
			name:      "Invalid text mixed",
			text:      "some text BV1GJ411x7h7",
			wantId:    "",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotId, gotMatch := matcher.MatchText(tt.text)
			if gotId != tt.wantId {
				t.Errorf("MatchText() gotId = %v, want %v", gotId, tt.wantId)
			}
			if gotMatch != tt.wantMatch {
				t.Errorf("MatchText() gotMatch = %v, want %v", gotMatch, tt.wantMatch)
			}
		})
	}
}
