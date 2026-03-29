package kugou

import (
	"encoding/json"
	"time"
)

type conceptJSONText string

func (t *conceptJSONText) UnmarshalJSON(data []byte) error {
	if t == nil {
		return nil
	}
	if string(data) == "null" {
		*t = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*t = conceptJSONText(s)
		return nil
	}
	var num json.Number
	if err := json.Unmarshal(data, &num); err == nil {
		*t = conceptJSONText(num.String())
		return nil
	}
	*t = conceptJSONText(string(data))
	return nil
}

type conceptBaseResponse struct {
	Status  int    `json:"status"`
	ErrCode int    `json:"errcode,omitempty"`
	Error   string `json:"error,omitempty"`
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type conceptDeviceInfo struct {
	Dfid      string `json:"dfid,omitempty"`
	Mid       string `json:"mid,omitempty"`
	Guid      string `json:"guid,omitempty"`
	Dev       string `json:"serverDev,omitempty"`
	Mac       string `json:"mac,omitempty"`
	Source    string `json:"source,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type conceptSession struct {
	Enabled           bool      `json:"enabled"`
	Token             string    `json:"token,omitempty"`
	UserID            string    `json:"userid,omitempty"`
	T1                string    `json:"t1,omitempty"`
	VIPType           string    `json:"vip_type,omitempty"`
	VIPToken          string    `json:"vip_token,omitempty"`
	Nickname          string    `json:"nickname,omitempty"`
	Cookie            string    `json:"cookie,omitempty"`
	QRKey             string    `json:"qr_key,omitempty"`
	QRURL             string    `json:"qr_url,omitempty"`
	QRBase64          string    `json:"qr_base64,omitempty"`
	QRStatus          int       `json:"qr_status,omitempty"`
	QRNickname        string    `json:"qr_nickname,omitempty"`
	LastCheckTime     time.Time `json:"last_check_time,omitempty"`
	LastRefreshTime   time.Time `json:"last_refresh_time,omitempty"`
	LastSignTime      time.Time `json:"last_sign_time,omitempty"`
	LastVIPClaimTime  time.Time `json:"last_vip_claim_time,omitempty"`
	LoginTime         time.Time `json:"login_time,omitempty"`
	SessionSource     string    `json:"session_source,omitempty"`
	VIPExpireTime     string    `json:"vip_expire_time,omitempty"`
	AutoRefresh       bool      `json:"auto_refresh"`
	AutoRefreshPeriod time.Duration
	Device            conceptDeviceInfo `json:"device"`
}

type conceptQRKeyData struct {
	QRCode string `json:"qrcode"`
}

type conceptQRCreateData struct {
	URL    string `json:"url"`
	Base64 string `json:"base64"`
}

type conceptQRCheckData struct {
	Status   int             `json:"status"`
	Token    conceptJSONText `json:"token,omitempty"`
	UserID   conceptJSONText `json:"userid,omitempty"`
	Nickname conceptJSONText `json:"nickname,omitempty"`
}

type conceptLoginTokenData struct {
	Token    string `json:"token,omitempty"`
	UserID   string `json:"userid,omitempty"`
	T1       string `json:"t1,omitempty"`
	VIPType  any    `json:"vip_type,omitempty"`
	VIPToken string `json:"vip_token,omitempty"`
}

type conceptSongURLResponse struct {
	Status     int            `json:"status"`
	ErrCode    int            `json:"errcode,omitempty"`
	Error      string         `json:"error,omitempty"`
	URL        []string       `json:"url,omitempty"`
	TimeLength int64          `json:"timeLength,omitempty"`
	ExtName    string         `json:"extName,omitempty"`
	Volume     float64        `json:"volume,omitempty"`
	VolumeGain float64        `json:"volume_gain,omitempty"`
	VolumePeak float64        `json:"volume_peak,omitempty"`
	Raw        map[string]any `json:"-"`
}

type conceptSongURLNewResponse struct {
	Status  int             `json:"status"`
	ErrCode int             `json:"errcode,omitempty"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Raw     json.RawMessage `json:"-"`
}

type conceptUserDetailData struct {
	Nickname string         `json:"nickname,omitempty"`
	Raw      map[string]any `json:"raw,omitempty"`
}

type conceptVIPDetailData struct {
	Raw map[string]any `json:"raw,omitempty"`
}

type conceptUnionVIPData struct {
	Raw map[string]any `json:"raw,omitempty"`
}
