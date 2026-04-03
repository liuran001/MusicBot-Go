package qqmusic

import "testing"

func TestParseQQAuth_FallbackToQMKeyst(t *testing.T) {
	cookie := "uin=12345; qm_keyst=fallback-key"

	uin, authst := parseQQAuth(cookie)
	if uin != "12345" {
		t.Fatalf("expected uin 12345, got %q", uin)
	}
	if authst != "fallback-key" {
		t.Fatalf("expected qm_keyst fallback, got %q", authst)
	}
}

func TestParseQQAuth_PreferQQMusicKey(t *testing.T) {
	cookie := "uin=12345; qqmusic_key=primary-key; qm_keyst=fallback-key"

	uin, authst := parseQQAuth(cookie)
	if uin != "12345" {
		t.Fatalf("expected uin 12345, got %q", uin)
	}
	if authst != "primary-key" {
		t.Fatalf("expected qqmusic_key preferred, got %q", authst)
	}
}

func TestParseQQAuth_NormalizeUIN(t *testing.T) {
	tests := []struct {
		name   string
		cookie string
		want   string
	}{
		{name: "prefixed", cookie: "uin=o123456; qqmusic_key=k", want: "123456"},
		{name: "plain", cookie: "uin=00123456; qqmusic_key=k", want: "123456"},
		{name: "invalid", cookie: "uin=abc123; qqmusic_key=k", want: "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uin, _ := parseQQAuth(tt.cookie)
			if uin != tt.want {
				t.Fatalf("expected uin %q, got %q", tt.want, uin)
			}
		})
	}
}
