package qqmusic

type qqSinger struct {
	ID   int64  `json:"id"`
	Mid  string `json:"mid"`
	Name string `json:"name"`
}

type qqAlbum struct {
	ID    int64  `json:"id"`
	Mid   string `json:"mid"`
	Name  string `json:"name"`
	Title string `json:"title"`
}

type qqSearchSong struct {
	SongID    int64      `json:"songid"`
	SongMID   string     `json:"songmid"`
	SongName  string     `json:"songname"`
	AlbumName string     `json:"albumname"`
	AlbumMID  string     `json:"albummid"`
	Interval  int        `json:"interval"`
	Singer    []qqSinger `json:"singer"`
}

type qqSearchSongAny struct {
	SongID    int64      `json:"songid"`
	ID        int64      `json:"id"`
	SongMID   string     `json:"songmid"`
	Mid       string     `json:"mid"`
	SongName  string     `json:"songname"`
	Name      string     `json:"name"`
	Title     string     `json:"title"`
	AlbumName string     `json:"albumname"`
	AlbumMID  string     `json:"albummid"`
	Album     qqAlbum    `json:"album"`
	Interval  int        `json:"interval"`
	Singer    []qqSinger `json:"singer"`
}

type qqSearchSongMobile struct {
	ID       int64      `json:"id"`
	Mid      string     `json:"mid"`
	Name     string     `json:"name"`
	Album    qqAlbum    `json:"album"`
	Singer   []qqSinger `json:"singer"`
	Interval int        `json:"interval"`
}

type qqSongDetail struct {
	ID       int64      `json:"id"`
	Mid      string     `json:"mid"`
	Name     string     `json:"name"`
	Title    string     `json:"title"`
	Album    qqAlbum    `json:"album"`
	Singer   []qqSinger `json:"singer"`
	Interval int        `json:"interval"`
}

type qqFileInfo struct {
	MediaMid  string `json:"media_mid"`
	Size128   int64  `json:"size_128mp3"`
	Size320   int64  `json:"size_320mp3"`
	SizeFlac  int64  `json:"size_flac"`
	SizeHiRes int64  `json:"size_hires"`
	CoverMid  string `json:"-"`
}

type qqPlaylistSong struct {
	SongID    int64      `json:"songid"`
	ID        int64      `json:"id"`
	SongMID   string     `json:"songmid"`
	Mid       string     `json:"mid"`
	SongName  string     `json:"songname"`
	Name      string     `json:"name"`
	Title     string     `json:"title"`
	AlbumName string     `json:"albumname"`
	AlbumMID  string     `json:"albummid"`
	Album     qqAlbum    `json:"album"`
	Interval  int        `json:"interval"`
	Singer    []qqSinger `json:"singer"`
}

type qqPlaylistCreator struct {
	Name string `json:"name"`
}

type qqPlaylistData struct {
	ID          int64             `json:"disstid"`
	Name        string            `json:"dissname"`
	Desc        string            `json:"desc"`
	Logo        string            `json:"logo"`
	Total       int               `json:"total_song_num"`
	Songlist    []qqPlaylistSong  `json:"songlist"`
	Creator     qqPlaylistCreator `json:"creator"`
	CreatorName string            `json:"creator_name"`
}

type qqAlbumSongItem struct {
	SongInfo qqPlaylistSong `json:"songInfo"`
}

type qqAlbumData struct {
	ID       string
	Mid      string
	Name     string
	Desc     string
	CoverURL string
	Creator  string
	Artists  []qqSinger
	Total    int
	Songlist []qqPlaylistSong
}
