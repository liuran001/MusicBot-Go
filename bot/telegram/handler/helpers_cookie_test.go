package handler

import "testing"

func TestLooksLikeCookiePayload(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "empty", text: "", want: false},
		{name: "normal query", text: "洛天依 soda", want: false},
		{name: "cookie pairs", text: "sessionid=abc; sid_tt=def; uid_tt=ghi", want: true},
		{name: "uifid payload", text: "UIFID=abc; stream_recommend_feed_params=xyz; ttwid=foo", want: true},
		{name: "single pair", text: "foo=bar", want: false},
	}
	for _, tt := range tests {
		if got := looksLikeCookiePayload(tt.text); got != tt.want {
			t.Fatalf("%s: looksLikeCookiePayload(%q) = %v, want %v", tt.name, tt.text, got, tt.want)
		}
	}
}
