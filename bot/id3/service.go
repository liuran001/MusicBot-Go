package id3

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	marker "github.com/XiaoMengXinX/163KeyMarker"
	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"go.senan.xyz/taglib"
)

type ID3Service struct {
	logger botpkg.Logger
}

func NewID3Service(logger botpkg.Logger) *ID3Service {
	return &ID3Service{logger: logger}
}

func (s *ID3Service) EmbedTags(audioPath string, tag *TagData, coverPath string) error {
	if tag == nil {
		return nil
	}

	ext := strings.ToLower(filepath.Ext(audioPath))
	if !isSupportedTagExtension(ext) {
		return errors.New("unsupported audio format for tags")
	}

	workingTag := cloneTagData(tag)
	if workingTag == nil {
		workingTag = &TagData{}
	}

	if markerData, ok := extractNeteaseMarker(workingTag); ok {
		applyMarkerToTagData(workingTag, markerData)
		if s.logger != nil {
			s.logger.Debug("embedded 163key marker", "format", ext, "key_length", len(marker.Create163KeyStr(markerData)))
		}
	}

	if err := s.writeTagsWithTaglib(audioPath, workingTag); err != nil {
		return err
	}

	if err := s.writeCoverWithTaglib(audioPath, coverPath, ext); err != nil {
		return err
	}

	return nil
}

func isSupportedTagExtension(ext string) bool {
	switch ext {
	case ".mp3", ".flac", ".m4a", ".mp4":
		return true
	default:
		return false
	}
}

func (s *ID3Service) writeTagsWithTaglib(audioPath string, tagData *TagData) error {
	tags := buildTaglibTags(tagData)
	if len(tags) == 0 {
		return nil
	}
	return taglib.WriteTags(audioPath, tags, 0)
}

func (s *ID3Service) writeCoverWithTaglib(audioPath, coverPath, ext string) error {
	if strings.TrimSpace(coverPath) == "" {
		return nil
	}

	artwork, err := readCoverWithLimit(coverPath, 10*1024*1024)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to read cover for tag embedding", "format", ext, "error", err)
		}
		return nil
	}

	if len(artwork) == 0 {
		return nil
	}

	return taglib.WriteImage(audioPath, artwork)
}

func buildTaglibTags(tagData *TagData) map[string][]string {
	tags := make(map[string][]string)
	if tagData == nil {
		return tags
	}

	addTaglibValue(tags, taglib.Title, tagData.Title)
	addTaglibValue(tags, taglib.Artist, tagData.Artist)
	addTaglibValue(tags, taglib.Album, tagData.Album)
	addTaglibValue(tags, taglib.AlbumArtist, tagData.AlbumArtist)
	addTaglibValue(tags, taglib.Date, tagData.Year)
	addTaglibValue(tags, taglib.Genre, tagData.Genre)
	addTaglibValue(tags, taglib.Comment, tagData.Comment)

	if tagData.TrackNumber > 0 {
		tags[taglib.TrackNumber] = []string{strconv.Itoa(tagData.TrackNumber)}
	}
	if tagData.DiscNumber > 0 {
		tags[taglib.DiscNumber] = []string{strconv.Itoa(tagData.DiscNumber)}
	}

	if lyrics := normalizedLyrics(tagData); lyrics != "" {
		tags[taglib.Lyrics] = []string{lyrics}
	}

	return tags
}

func addTaglibValue(tags map[string][]string, key, value string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return
	}
	tags[key] = []string{trimmed}
}

func normalizedLyrics(tagData *TagData) string {
	if tagData == nil {
		return ""
	}
	return platform.NormalizeLRCTimestamps(tagData.Lyrics)
}

func cloneTagData(tagData *TagData) *TagData {
	if tagData == nil {
		return nil
	}
	cloned := *tagData
	if tagData.Extra != nil {
		cloned.Extra = make(map[string]any, len(tagData.Extra))
		for k, v := range tagData.Extra {
			cloned.Extra[k] = v
		}
	}
	return &cloned
}

func extractNeteaseMarker(tagData *TagData) (marker.MarkerData, bool) {
	if tagData == nil || tagData.Extra == nil {
		return marker.MarkerData{}, false
	}
	markerData, ok := tagData.Extra["netease_marker"].(marker.MarkerData)
	if !ok {
		return marker.MarkerData{}, false
	}
	return markerData, true
}

func applyMarkerToTagData(tagData *TagData, markerData marker.MarkerData) {
	if tagData == nil {
		return
	}
	key163 := marker.Create163KeyStr(markerData)
	if strings.TrimSpace(tagData.Comment) == "" {
		tagData.Comment = key163
		return
	}
	tagData.Comment = tagData.Comment + "\n" + key163
}

func readCoverWithLimit(path string, maxSize int64) ([]byte, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if stat.Size() > maxSize {
		return nil, fmt.Errorf("cover image too large: %d bytes (max %d)", stat.Size(), maxSize)
	}
	return os.ReadFile(path)
}
