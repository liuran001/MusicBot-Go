package id3

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	marker "github.com/XiaoMengXinX/163KeyMarker"
	"github.com/bogem/id3v2"
	"github.com/go-flac/flacpicture"
	"github.com/go-flac/flacvorbis"
	"github.com/go-flac/go-flac"
	botpkg "github.com/liuran001/MusicBot-Go/bot"
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
	if tag.Extra != nil {
		if markerData, ok := tag.Extra["netease_marker"].(marker.MarkerData); ok && ext == ".mp3" {
			if err := s.embedMarkerTags(audioPath, markerData, coverPath); err != nil {
				return err
			}
			return s.embedMp3Lyrics(audioPath, tag)
		}
	}

	switch ext {
	case ".mp3":
		return s.embedMp3Tags(audioPath, tag, coverPath)
	case ".flac":
		return s.embedFlacTags(audioPath, tag, coverPath)
	default:
		return errors.New("unsupported audio format for tags")
	}
}

func (s *ID3Service) embedMarkerTags(audioPath string, markerData marker.MarkerData, coverPath string) error {
	file, err := os.Open(audioPath)
	if err != nil {
		return err
	}
	defer file.Close()

	var pic *os.File
	if coverPath != "" {
		pic, _ = os.Open(coverPath)
	}

	err = marker.AddMusicID3V2(file, pic, markerData)
	if pic != nil {
		_ = pic.Close()
	}
	if err == nil {
		return nil
	}

	if coverPath != "" {
		file, err = os.Open(audioPath)
		if err != nil {
			return err
		}
		defer file.Close()
		return marker.AddMusicID3V2(file, nil, markerData)
	}

	return err
}

func (s *ID3Service) embedMp3Tags(audioPath string, tagData *TagData, coverPath string) error {
	meta, err := id3v2.Open(audioPath, id3v2.Options{Parse: true})
	if err != nil {
		return err
	}
	defer meta.Close()

	if tagData.Title != "" {
		meta.SetTitle(tagData.Title)
	}
	if tagData.Artist != "" {
		meta.SetArtist(tagData.Artist)
	}
	if tagData.Album != "" {
		meta.SetAlbum(tagData.Album)
	}
	if tagData.AlbumArtist != "" {
		meta.AddTextFrame("TPE2", id3v2.EncodingUTF8, tagData.AlbumArtist)
	}
	if tagData.Year != "" {
		meta.AddTextFrame("TDRC", id3v2.EncodingUTF8, tagData.Year)
	}
	if tagData.TrackNumber > 0 {
		meta.AddTextFrame("TRCK", id3v2.EncodingUTF8, fmt.Sprintf("%d", tagData.TrackNumber))
	}
	if tagData.DiscNumber > 0 {
		meta.AddTextFrame("TPOS", id3v2.EncodingUTF8, fmt.Sprintf("%d", tagData.DiscNumber))
	}
	if tagData.Genre != "" {
		meta.SetGenre(tagData.Genre)
	}
	if tagData.Comment != "" {
		meta.AddCommentFrame(id3v2.CommentFrame{
			Encoding: id3v2.EncodingUTF8,
			Language: "eng",
			Text:     tagData.Comment,
		})
	}
	if tagData.Lyrics != "" {
		meta.AddUnsynchronisedLyricsFrame(id3v2.UnsynchronisedLyricsFrame{
			Encoding:          id3v2.EncodingUTF8,
			Language:          "und",
			ContentDescriptor: "LRC",
			Lyrics:            tagData.Lyrics,
		})
	}

	if coverPath != "" {
		artwork, err := os.ReadFile(coverPath)
		if err == nil && len(artwork) > 0 {
			mime := http.DetectContentType(artwork[:minInt(len(artwork), 32)])
			pic := id3v2.PictureFrame{
				Encoding:    id3v2.EncodingISO,
				MimeType:    mime,
				PictureType: id3v2.PTFrontCover,
				Description: "Front cover",
				Picture:     artwork,
			}
			meta.AddAttachedPicture(pic)
		}
	}

	return meta.Save()
}

func (s *ID3Service) embedFlacTags(audioPath string, tagData *TagData, coverPath string) error {
	file, err := os.Open(audioPath)
	if err != nil {
		return err
	}
	defer file.Close()

	parsed, err := flac.ParseMetadata(file)
	if err != nil {
		return err
	}

	if coverPath != "" {
		artwork, err := os.ReadFile(coverPath)
		if err == nil && len(artwork) > 0 {
			mime := http.DetectContentType(artwork[:minInt(len(artwork), 32)])
			picture, err := flacpicture.NewFromImageData(flacpicture.PictureTypeFrontCover, "Front cover", artwork, mime)
			if err == nil {
				pictureMeta := picture.Marshal()
				parsed.Meta = append(parsed.Meta, &pictureMeta)
			}
		}
	}

	vorbis := flacvorbis.New()
	if tagData.Title != "" {
		_ = vorbis.Add(flacvorbis.FIELD_TITLE, tagData.Title)
	}
	if tagData.Artist != "" {
		_ = vorbis.Add(flacvorbis.FIELD_ARTIST, tagData.Artist)
	}
	if tagData.Album != "" {
		_ = vorbis.Add(flacvorbis.FIELD_ALBUM, tagData.Album)
	}
	if tagData.AlbumArtist != "" {
		_ = vorbis.Add("ALBUMARTIST", tagData.AlbumArtist)
	}
	if tagData.Year != "" {
		_ = vorbis.Add("DATE", tagData.Year)
	}
	if tagData.TrackNumber > 0 {
		_ = vorbis.Add("TRACKNUMBER", fmt.Sprintf("%d", tagData.TrackNumber))
	}
	if tagData.DiscNumber > 0 {
		_ = vorbis.Add("DISCNUMBER", fmt.Sprintf("%d", tagData.DiscNumber))
	}
	if tagData.Genre != "" {
		_ = vorbis.Add("GENRE", tagData.Genre)
	}
	if tagData.Comment != "" {
		_ = vorbis.Add("COMMENT", tagData.Comment)
	}
	if tagData.Lyrics != "" {
		_ = vorbis.Add("LYRICS", tagData.Lyrics)
	}

	meta := vorbis.Marshal()
	idx := -1
	for i, m := range parsed.Meta {
		if m.Type == flac.VorbisComment {
			idx = i
			break
		}
	}
	if idx >= 0 {
		parsed.Meta[idx] = &meta
	} else {
		parsed.Meta = append(parsed.Meta, &meta)
	}

	return saveFlacWithMeta(audioPath, parsed)
}

func (s *ID3Service) embedMp3Lyrics(audioPath string, tagData *TagData) error {
	if tagData == nil || tagData.Lyrics == "" {
		return nil
	}

	meta, err := id3v2.Open(audioPath, id3v2.Options{Parse: true})
	if err != nil {
		return err
	}
	defer meta.Close()

	meta.AddUnsynchronisedLyricsFrame(id3v2.UnsynchronisedLyricsFrame{
		Encoding:          id3v2.EncodingUTF8,
		Language:          "und",
		ContentDescriptor: "LRC",
		Lyrics:            tagData.Lyrics,
	})

	return meta.Save()
}

func saveFlacWithMeta(audioPath string, file *flac.File) error {
	original, err := os.Open(audioPath)
	if err != nil {
		return err
	}
	defer original.Close()

	stat, err := original.Stat()
	if err != nil {
		return err
	}

	tmpPath := audioPath + "-id3v2"
	out, err := os.OpenFile(tmpPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, stat.Mode())
	if err != nil {
		return err
	}

	defer func() {
		_ = out.Close()
	}()

	if _, err := out.Write([]byte("fLaC")); err != nil {
		return err
	}
	for i, meta := range file.Meta {
		last := i == len(file.Meta)-1
		if _, err := out.Write(meta.Marshal(last)); err != nil {
			return err
		}
	}

	if _, err := original.Seek(4, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.Copy(out, original); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, audioPath); err != nil {
		return err
	}

	return nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
