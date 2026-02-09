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
	"github.com/liuran001/MusicBot-Go/bot/platform"
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
		if markerData, ok := tag.Extra["netease_marker"].(marker.MarkerData); ok {
			if ext == ".mp3" {
				return s.embedMp3TagsWithMarker(audioPath, tag, coverPath, markerData)
			} else if ext == ".flac" {
				return s.embedFlacTagsWithMarker(audioPath, tag, coverPath, markerData)
			}
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

func (s *ID3Service) writeMp3BasicTags(meta *id3v2.Tag, tagData *TagData) {
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
}

func (s *ID3Service) writeMp3Lyrics(meta *id3v2.Tag, tagData *TagData, logMsg string) {
	lyrics := platform.NormalizeLRCTimestamps(tagData.Lyrics)
	if lyrics != "" {
		meta.AddUnsynchronisedLyricsFrame(id3v2.UnsynchronisedLyricsFrame{
			Encoding:          id3v2.EncodingUTF8,
			Language:          "und",
			ContentDescriptor: "LRC",
			Lyrics:            lyrics,
		})
		if s.logger != nil {
			s.logger.Info(logMsg, "lyrics_length", len(lyrics))
		}
	} else if s.logger != nil {
		s.logger.Warn("mp3 lyrics field is empty, skipping lyrics embedding")
	}
}

func (s *ID3Service) writeMp3Cover(meta *id3v2.Tag, coverPath string) {
	if coverPath != "" {
		artwork, err := readCoverWithLimit(coverPath, 10*1024*1024)
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
		} else if err != nil && s.logger != nil {
			s.logger.Warn("failed to read cover for mp3 embedding", "error", err)
		}
	}
}

func (s *ID3Service) embedMp3Tags(audioPath string, tagData *TagData, coverPath string) error {
	meta, err := id3v2.Open(audioPath, id3v2.Options{Parse: true})
	if err != nil {
		return err
	}
	defer meta.Close()

	meta.SetDefaultEncoding(id3v2.EncodingUTF8)
	s.writeMp3BasicTags(meta, tagData)
	s.writeMp3Lyrics(meta, tagData, "embedded mp3 lyrics")
	s.writeMp3Cover(meta, coverPath)

	return meta.Save()
}

func (s *ID3Service) writeFlacCover(parsed *flac.File, coverPath string) {
	if coverPath != "" {
		artwork, err := readCoverWithLimit(coverPath, 10*1024*1024)
		if err == nil && len(artwork) > 0 {
			mime := http.DetectContentType(artwork[:minInt(len(artwork), 32)])
			picture, err := flacpicture.NewFromImageData(flacpicture.PictureTypeFrontCover, "", artwork, mime)
			if err == nil {
				cmt := picture.Marshal()
				parsed.Meta = append(parsed.Meta, &cmt)
			} else if s.logger != nil {
				s.logger.Warn("failed to create flac picture", "error", err)
			}
		} else if err != nil && s.logger != nil {
			s.logger.Warn("failed to read cover for flac embedding", "error", err)
		}
	}
}

func (s *ID3Service) writeFlacBasicTags(vorbis *flacvorbis.MetaDataBlockVorbisComment, tagData *TagData) {
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
}

func (s *ID3Service) writeFlacLyrics(vorbis *flacvorbis.MetaDataBlockVorbisComment, tagData *TagData, logMsg string) {
	lyrics := platform.NormalizeLRCTimestamps(tagData.Lyrics)
	if lyrics != "" {
		_ = vorbis.Add("LYRICS", lyrics)
		if s.logger != nil {
			s.logger.Info(logMsg, "lyrics_length", len(lyrics))
		}
	} else if s.logger != nil {
		s.logger.Warn("flac lyrics field is empty, skipping lyrics embedding")
	}
}

func (s *ID3Service) setFlacVorbisComment(parsed *flac.File, vorbis *flacvorbis.MetaDataBlockVorbisComment) {
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

	s.writeFlacCover(parsed, coverPath)

	vorbis := flacvorbis.New()
	s.writeFlacBasicTags(vorbis, tagData)
	s.writeFlacLyrics(vorbis, tagData, "embedded flac lyrics")
	s.setFlacVorbisComment(parsed, vorbis)

	return saveFlacWithMeta(audioPath, parsed)
}

func (s *ID3Service) embedFlacTagsWithMarker(audioPath string, tagData *TagData, coverPath string, markerData marker.MarkerData) error {
	file, err := os.Open(audioPath)
	if err != nil {
		return err
	}
	defer file.Close()

	parsed, err := flac.ParseMetadata(file)
	if err != nil {
		return err
	}

	s.writeFlacCover(parsed, coverPath)

	vorbis := flacvorbis.New()
	s.writeFlacBasicTags(vorbis, tagData)
	s.writeFlacLyrics(vorbis, tagData, "embedded flac lyrics with marker")

	key163 := marker.Create163KeyStr(markerData)
	_ = vorbis.Add(flacvorbis.FIELD_DESCRIPTION, key163)
	if s.logger != nil {
		s.logger.Info("embedded 163key marker in flac", "key_length", len(key163))
	}

	s.setFlacVorbisComment(parsed, vorbis)

	return saveFlacWithMeta(audioPath, parsed)
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

func (s *ID3Service) embedMp3TagsWithMarker(audioPath string, tagData *TagData, coverPath string, markerData marker.MarkerData) error {
	meta, err := id3v2.Open(audioPath, id3v2.Options{Parse: true})
	if err != nil {
		return err
	}
	defer meta.Close()

	meta.SetDefaultEncoding(id3v2.EncodingUTF8)
	s.writeMp3BasicTags(meta, tagData)
	s.writeMp3Lyrics(meta, tagData, "embedded mp3 lyrics with marker")

	key163 := marker.Create163KeyStr(markerData)
	comment := id3v2.CommentFrame{
		Encoding:    id3v2.EncodingISO,
		Language:    "chs",
		Description: "",
		Text:        key163,
	}
	meta.AddCommentFrame(comment)
	if s.logger != nil {
		s.logger.Info("embedded 163key marker in mp3", "key_length", len(key163))
	}

	s.writeMp3Cover(meta, coverPath)

	return meta.Save()
}
