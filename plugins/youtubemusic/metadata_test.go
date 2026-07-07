package youtubemusic

import "testing"

func TestMetadataLimitsPassiveGroupURLHosts(t *testing.T) {
	meta := metadata()
	if !meta.AllowGroupURL {
		t.Fatal("expected YouTube Music links to remain allowed for passive group URL parsing")
	}
	if len(meta.GroupURLHosts) != 1 || meta.GroupURLHosts[0] != "music.youtube.com" {
		t.Fatalf("GroupURLHosts = %#v, want [music.youtube.com]", meta.GroupURLHosts)
	}
}
