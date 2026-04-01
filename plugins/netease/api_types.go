package netease

import "net/http"

type Header struct {
	Name  string
	Value string
}

type Headers []Header

type RequestData struct {
	Cookies []*http.Cookie
	Headers Headers
	Body    string
	Client  *http.Client
}

type EAPIOption struct {
	Json string
	Path string
	Url  string
}

type SongsDetailData struct {
	RawJson    string           `json:"-"`
	Songs      []SongDetailData `json:"songs"`
	Privileges []struct {
		Id int `json:"id"`
	} `json:"privileges"`
	Code int `json:"code"`
}

type SongDetailData struct {
	Name string `json:"name"`
	Id   int    `json:"id"`
	Pst  int    `json:"pst"`
	T    int    `json:"t"`
	Ar   []struct {
		Id   int    `json:"id"`
		Name string `json:"name"`
	} `json:"ar"`
	Alia []interface{} `json:"alia"`
	Pop  float64       `json:"pop"`
	St   int           `json:"st"`
	Rt   interface{}   `json:"rt"`
	Fee  int           `json:"fee"`
	V    int           `json:"v"`
	Crbt interface{}   `json:"crbt"`
	Cf   string        `json:"cf"`
	Al   struct {
		Id     int           `json:"id"`
		Name   string        `json:"name"`
		PicUrl string        `json:"picUrl"`
		Tns    []interface{} `json:"tns"`
		PicStr string        `json:"pic_str"`
		Pic    int           `json:"pic"`
	} `json:"al"`
	Dt                   int         `json:"dt"`
	Cd                   string      `json:"cd"`
	No                   int         `json:"no"`
	DjId                 int         `json:"djId"`
	Copyright            int         `json:"copyright"`
	SId                  int         `json:"s_id"`
	Mark                 int         `json:"mark"`
	OriginCoverType      int         `json:"originCoverType"`
	OriginSongSimpleData interface{} `json:"originSongSimpleData"`
	ResourceState        bool        `json:"resourceState"`
	Version              int         `json:"version"`
	Single               int         `json:"single"`
	NoCopyrightRcmd      interface{} `json:"noCopyrightRcmd"`
	Mv                   int         `json:"mv"`
	Rtype                int         `json:"rtype"`
	Rurl                 interface{} `json:"rurl"`
	Mst                  int         `json:"mst"`
	Cp                   int         `json:"cp"`
	PublishTime          int         `json:"publishTime"`
}

type SongsURLData struct {
	RawJson string        `json:"-"`
	Data    []SongURLData `json:"data"`
	Code    int           `json:"code"`
}

type SongURLData struct {
	Id        int    `json:"id"`
	Url       string `json:"url"`
	Br        int    `json:"br"`
	Size      int    `json:"size"`
	Md5       string `json:"md5"`
	Code      int    `json:"code"`
	Expi      int    `json:"expi"`
	Type      string `json:"type"`
	UrlSource int    `json:"urlSource"`
}

type SongLyricData struct {
	RawJson string `json:"-"`
	Sgc     bool   `json:"sgc"`
	Sfy     bool   `json:"sfy"`
	Qfy     bool   `json:"qfy"`
	Lrc     struct {
		Version int    `json:"version"`
		Lyric   string `json:"lyric"`
	} `json:"lrc"`
	Klyric struct {
		Version int    `json:"version"`
		Lyric   string `json:"lyric"`
	} `json:"klyric"`
	Tlyric struct {
		Version int    `json:"version"`
		Lyric   string `json:"lyric"`
	} `json:"tlyric"`
	Code int `json:"code"`
}

type SearchSongData struct {
	RawJson string `json:"-"`
	Result  struct {
		Songs []SearchSongItem `json:"songs"`
	} `json:"result"`
	Code int `json:"code"`
}

type SearchSongItem struct {
	Id      int    `json:"id"`
	Name    string `json:"name"`
	Artists []struct {
		Id        int           `json:"id"`
		Name      string        `json:"name"`
		PicUrl    interface{}   `json:"picUrl"`
		Alias     []interface{} `json:"alias"`
		AlbumSize int           `json:"albumSize"`
		PicId     int           `json:"picId"`
		Img1V1Url string        `json:"img1v1Url"`
		Img1V1    int           `json:"img1v1"`
		Trans     interface{}   `json:"trans"`
	} `json:"artists"`
	Album struct {
		Id     int    `json:"id"`
		Name   string `json:"name"`
		Artist struct {
			Id        int           `json:"id"`
			Name      string        `json:"name"`
			PicUrl    interface{}   `json:"picUrl"`
			Alias     []interface{} `json:"alias"`
			AlbumSize int           `json:"albumSize"`
			PicId     int           `json:"picId"`
			Img1V1Url string        `json:"img1v1Url"`
			Img1V1    int           `json:"img1v1"`
			Trans     interface{}   `json:"trans"`
		} `json:"artist"`
		PublishTime int64 `json:"publishTime"`
		Size        int   `json:"size"`
		CopyrightId int   `json:"copyrightId"`
		Status      int   `json:"status"`
		PicId       int64 `json:"picId"`
		Mark        int   `json:"mark"`
	} `json:"album"`
	Duration    int           `json:"duration"`
	CopyrightId int           `json:"copyrightId"`
	Status      int           `json:"status"`
	Alias       []interface{} `json:"alias"`
	Rtype       int           `json:"rtype"`
	Ftype       int           `json:"ftype"`
	Mvid        int           `json:"mvid"`
	Fee         int           `json:"fee"`
	RUrl        interface{}   `json:"rUrl"`
	Mark        int           `json:"mark"`
}

type PlaylistDetailData struct {
	RawJson  string `json:"-"`
	Code     int    `json:"code"`
	Playlist struct {
		Id          int         `json:"id"`
		Name        string      `json:"name"`
		CoverImgUrl string      `json:"coverImgUrl"`
		Description interface{} `json:"description"`
		TrackCount  int         `json:"trackCount"`
		PlayCount   int         `json:"playCount"`
		Creator     struct {
			UserId   int    `json:"userId"`
			Nickname string `json:"nickname"`
		} `json:"creator"`
		Tracks []struct {
			Name string `json:"name"`
			Id   int    `json:"id"`
			Ar   []struct {
				Id   int    `json:"id"`
				Name string `json:"name"`
			} `json:"ar"`
			Alia []string `json:"alia"`
			Al   struct {
				Id     int    `json:"id"`
				Name   string `json:"name"`
				PicUrl string `json:"picUrl"`
			} `json:"al"`
			Dt                   int         `json:"dt"`
			Fee                  int         `json:"fee"`
			Copyright            int         `json:"copyright"`
			SId                  int         `json:"s_id"`
			Mark                 int         `json:"mark"`
			OriginCoverType      int         `json:"originCoverType"`
			OriginSongSimpleData interface{} `json:"originSongSimpleData"`
			Single               int         `json:"single"`
			NoCopyrightRcmd      *struct {
				Type     int     `json:"type"`
				TypeDesc string  `json:"typeDesc"`
				SongId   *string `json:"songId"`
			} `json:"noCopyrightRcmd"`
			Mst         int `json:"mst"`
			Cp          int `json:"cp"`
			Mv          int `json:"mv"`
			Rtype       int `json:"rtype"`
			PublishTime int `json:"publishTime"`
		} `json:"tracks"`
		TrackIds []struct {
			Id int `json:"id"`
		} `json:"trackIds"`
	} `json:"playlist"`
}

type AlbumDetailData struct {
	RawJson       string `json:"-"`
	Code          int    `json:"code"`
	ResourceState bool   `json:"resourceState"`
	Album         struct {
		Id          int    `json:"id"`
		Name        string `json:"name"`
		PicUrl      string `json:"picUrl"`
		Description string `json:"description"`
		BriefDesc   string `json:"briefDesc"`
		Size        int    `json:"size"`
		Artist      struct {
			Id   int    `json:"id"`
			Name string `json:"name"`
		} `json:"artist"`
		Artists []struct {
			Id   int    `json:"id"`
			Name string `json:"name"`
		} `json:"artists"`
	} `json:"album"`
	Songs []SongDetailData `json:"songs"`
}

type ProgramDetailData struct {
	RawJson string `json:"-"`
	Program struct {
		MainSong struct {
			Id int `json:"id"`
		} `json:"mainSong"`
	} `json:"program"`
	Code int `json:"code"`
}

type SearchSongConfig struct {
	Keyword string
	Limit   int
	Offset  int
}

type SongURLConfig struct {
	EncodeType string
	Level      string
	IDs        []int
}
