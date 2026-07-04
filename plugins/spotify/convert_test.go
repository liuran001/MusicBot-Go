package spotify

import (
	"testing"
	"time"
)

func TestConvertPathfinderTrack(t *testing.T) {
	var in pathfinderTrack
	in.ID = "track-id"
	in.Name = "Track"
	in.Duration.TotalMilliseconds = 123456
	in.TrackNumber = 4
	in.FirstArtist.Items = []pathfinderArtist{{ID: "artist-id"}}
	in.FirstArtist.Items[0].Profile.Name = "Artist"
	in.Album.ID = "album-id"
	in.Album.Name = "Album"
	in.Album.Date.ISOString = "2024-05-06T00:00:00Z"
	in.Album.Date.Precision = "DAY"
	in.Album.Date.Year = 2024
	in.Album.Tracks.TotalCount = 10
	in.Album.CoverArt.Sources = []spotifyImage{
		{URL: "small", Width: 64, Height: 64},
		{URL: "large", Width: 640, Height: 640},
	}

	got := convertPathfinderTrack(in)
	if got.ID != "track-id" || got.Title != "Track" {
		t.Fatalf("unexpected track identity: %+v", got)
	}
	if got.Duration != 123456*time.Millisecond {
		t.Fatalf("duration = %v", got.Duration)
	}
	if len(got.Artists) != 1 || got.Artists[0].Name != "Artist" {
		t.Fatalf("artists = %+v", got.Artists)
	}
	if got.Album == nil || got.Album.Title != "Album" {
		t.Fatalf("album = %+v", got.Album)
	}
	if got.CoverURL != "large" {
		t.Fatalf("cover = %q, want large", got.CoverURL)
	}
	if got.Album.ReleaseDate == nil || got.Album.ReleaseDate.Format("2006-01-02") != "2024-05-06" {
		t.Fatalf("release date = %v", got.Album.ReleaseDate)
	}
}
