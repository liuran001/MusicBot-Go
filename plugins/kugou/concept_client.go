package kugou

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/guohuiyuan/music-lib/model"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	qrcode "github.com/skip2/go-qrcode"
)

const (
	kugouConceptLoginBaseURL       = "https://login-user.kugou.com"
	kugouConceptLoginTokenURL      = "http://login.user.kugou.com/v5/login_by_token"
	kugouConceptGatewayBaseURL     = "https://gateway.kugou.com"
	kugouConceptUserServiceBaseURL = "https://userservice.kugou.com"
	kugouConceptVIPBaseURL         = "https://kugouvip.kugou.com"

	kugouConceptAppID       = "3116"
	kugouConceptClientVer   = "11440"
	kugouConceptSrcAppID    = "2919"
	kugouConceptQRAppID     = "1001"
	kugouConceptUserAgent   = "Android15-1070-11083-46-0-DiscoveryDRADProtocol-wifi"
	kugouConceptQRTextURL   = "https://h5.kugou.com/apps/loginQRCode/html/index.html?appid=3116&"
	kugouConceptRouteUser   = "usercenter.kugou.com"
	kugouConceptRouteTrack  = "trackercdn.kugou.com"
	kugouConceptKGTHash     = "5d816a0"
	kugouConceptKGRF        = "B9EDA08A64250DEFFBCADDEE00F8F25F"
	kugouConceptPageID      = "967177915"
	kugouConceptPPageID     = "356753938,823673182,967485191"
	kugouConceptSignSecret  = "LnT6xpN3khm36zse0QzvmgTZ3waWdRSA"
	kugouConceptPlaySecret  = "185672dd44712f60bb1736df5a377e82"
	kugouConceptRefreshKey  = "c24f74ca2820225badc01946dba4fdf7"
	kugouConceptRefreshIV   = "adc01946dba4fdf7"
	kugouConceptT2Key       = "fd14b35e3f81af3817a20ae7adae7020"
	kugouConceptT2IV        = "17a20ae7adae7020"
	kugouConceptT1Key       = "5e4ef500e9597fe004bd09a46d8add98"
	kugouConceptT1IV        = "04bd09a46d8add98"
	kugouConceptT2FixedPart = "0f607264fc6318a92b9e13c65db7cd3c"
	defaultConceptVIPType   = "6"
)

type ConceptAPIClient struct {
	http  *http.Client
	state *ConceptSessionManager
}

func NewConceptAPIClient(_ string, state *ConceptSessionManager) *ConceptAPIClient {
	return &ConceptAPIClient{
		http:  http.DefaultClient,
		state: state,
	}
}

func (c *ConceptAPIClient) SetHTTPClient(client *http.Client) {
	if c == nil {
		return
	}
	if client == nil {
		c.http = http.DefaultClient
		return
	}
	c.http = client
}

func (c *ConceptAPIClient) Enabled() bool {
	return c != nil && c.state != nil && c.state.Enabled()
}

func (c *ConceptAPIClient) HasUsableSession() bool {
	return c != nil && c.state != nil && c.state.HasUsableSession()
}

func (c *ConceptAPIClient) CookieString() string {
	if c == nil || c.state == nil {
		return ""
	}
	return c.state.CookieString()
}

func (c *ConceptAPIClient) CreateQRCode(ctx context.Context) (conceptQRCreateData, error) {
	if c == nil || c.state == nil {
		return conceptQRCreateData{}, fmt.Errorf("concept client unavailable")
	}
	if _, err := c.ensureDevice(ctx); err != nil {
		return conceptQRCreateData{}, err
	}
	state := c.state.Snapshot()
	query, _ := c.defaultQuery(state, time.Now(), true)
	query.Set("appid", kugouConceptQRAppID)
	query.Set("type", "1")
	query.Set("plat", "4")
	query.Set("qrcode_txt", kugouConceptQRTextURL)
	query.Set("srcappid", kugouConceptSrcAppID)
	query.Set("signature", conceptSignatureWeb(query))

	var keyResp struct {
		conceptBaseResponse
		Data conceptQRKeyData `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, kugouConceptLoginBaseURL+"/v2/qrcode?"+query.Encode(), nil, conceptSession{}, nil, &keyResp); err != nil {
		return conceptQRCreateData{}, err
	}
	if strings.TrimSpace(keyResp.Data.QRCode) == "" {
		return conceptQRCreateData{}, fmt.Errorf("二维码 key 为空")
	}
	data := conceptQRCreateData{
		URL: buildConceptQRCodeURL(keyResp.Data.QRCode),
	}
	if png, err := qrcode.Encode(data.URL, qrcode.Medium, 256); err == nil {
		data.Base64 = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	}
	now := time.Now()
	c.state.Update(func(state *conceptSession) {
		state.QRKey = keyResp.Data.QRCode
		state.QRURL = data.URL
		state.QRBase64 = ""
		state.QRStatus = 1
		state.LastCheckTime = now
		if state.SessionSource == "" {
			state.SessionSource = "concept_qr"
		}
		state.Cookie = c.buildConceptCookie(state)
	})
	_ = c.state.Persist()
	return data, nil
}

func (c *ConceptAPIClient) CheckQRCode(ctx context.Context) (conceptQRCheckData, error) {
	if c == nil || c.state == nil {
		return conceptQRCheckData{}, fmt.Errorf("concept client unavailable")
	}
	state := c.state.Snapshot()
	if strings.TrimSpace(state.QRKey) == "" {
		return conceptQRCheckData{}, fmt.Errorf("当前没有待扫码二维码")
	}
	query, _ := c.defaultQuery(state, time.Now(), true)
	query.Set("plat", "4")
	query.Set("appid", kugouConceptAppID)
	query.Set("srcappid", kugouConceptSrcAppID)
	query.Set("qrcode", state.QRKey)
	query.Set("signature", conceptSignatureWeb(query))

	var resp struct {
		conceptBaseResponse
		Data conceptQRCheckData `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, kugouConceptLoginBaseURL+"/v2/get_userinfo_qrcode?"+query.Encode(), nil, conceptSession{}, nil, &resp); err != nil {
		return conceptQRCheckData{}, err
	}
	now := time.Now()
	c.state.Update(func(s *conceptSession) {
		s.QRStatus = resp.Data.Status
		s.QRNickname = strings.TrimSpace(string(resp.Data.Nickname))
		s.LastCheckTime = now
		if resp.Data.Status == 4 {
			s.Token = strings.TrimSpace(string(resp.Data.Token))
			s.UserID = strings.TrimSpace(string(resp.Data.UserID))
			if strings.TrimSpace(string(resp.Data.Nickname)) != "" {
				s.Nickname = strings.TrimSpace(string(resp.Data.Nickname))
			}
			s.LoginTime = now
			s.SessionSource = "concept_qr"
			s.QRKey = ""
			s.QRURL = ""
			s.QRBase64 = ""
			s.Cookie = c.buildConceptCookie(s)
		} else if resp.Data.Status == 0 {
			s.QRKey = ""
			s.QRURL = ""
			s.QRBase64 = ""
			s.QRNickname = ""
		}
	})
	_ = c.state.Persist()
	return resp.Data, nil
}

func (c *ConceptAPIClient) ManualRenew(ctx context.Context) (string, error) {
	if c == nil || c.state == nil {
		return "", fmt.Errorf("concept client unavailable")
	}
	state := c.state.Snapshot()
	if strings.TrimSpace(state.Token) == "" || strings.TrimSpace(state.UserID) == "" {
		return "", fmt.Errorf("概念版会话不完整，请重新扫码")
	}
	if _, err := c.ensureDevice(ctx); err != nil {
		return "", err
	}
	state = c.state.Snapshot()
	nowMs := time.Now().UnixMilli()
	p3 := conceptAESCBCEncryptHex(map[string]any{
		"clienttime": nowMs / 1000,
		"token":      state.Token,
	}, kugouConceptRefreshKey, kugouConceptRefreshIV)
	paramsEnc, paramsKey, err := conceptAESCBCEncryptHexAuto(map[string]any{})
	if err != nil {
		return "", err
	}
	pkPayload := map[string]any{"clienttime_ms": nowMs, "key": paramsKey}
	pk, err := conceptRSARawEncryptHex(pkPayload, conceptLitePublicKeyPEM)
	if err != nil {
		return "", err
	}
	t2 := conceptAESCBCEncryptStringHex(strings.Join([]string{
		state.Device.Guid,
		kugouConceptT2FixedPart,
		state.Device.Mac,
		state.Device.Dev,
		strconv.FormatInt(nowMs, 10),
	}, "|"), kugouConceptT2Key, kugouConceptT2IV)
	t1Source := "|" + strconv.FormatInt(nowMs, 10)
	if strings.TrimSpace(state.T1) != "" {
		t1Source = state.T1 + "|" + strconv.FormatInt(nowMs, 10)
	}
	t1 := conceptAESCBCEncryptStringHex(t1Source, kugouConceptT1Key, kugouConceptT1IV)
	bodyMap := map[string]any{
		"dfid":          firstNonEmpty(state.Device.Dfid, "-"),
		"p3":            p3,
		"plat":          1,
		"t1":            t1,
		"t2":            t2,
		"t3":            "MCwwLDAsMCwwLDAsMCwwLDA=",
		"pk":            strings.ToUpper(pk),
		"params":        paramsEnc,
		"userid":        state.UserID,
		"clienttime_ms": nowMs,
		"dev":           state.Device.Dev,
	}
	query, _ := c.defaultQuery(state, time.UnixMilli(nowMs), false)
	bodyJSON, err := json.Marshal(bodyMap)
	if err != nil {
		return "", err
	}
	query.Set("signature", conceptSignatureAndroid(query, string(bodyJSON)))

	var resp struct {
		conceptBaseResponse
		Data map[string]any `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodPost, kugouConceptLoginTokenURL+"?"+query.Encode(), bytes.NewReader(bodyJSON), state, map[string]string{"Content-Type": "application/json"}, &resp); err != nil {
		return "", err
	}
	if secuParams := valueString(resp.Data["secu_params"]); secuParams != "" {
		if decoded, err := conceptAESCBCDecryptHex(secuParams, conceptMD5(paramsKey)[:32], conceptMD5(paramsKey)[16:32]); err == nil {
			var extra map[string]any
			if json.Unmarshal([]byte(decoded), &extra) == nil {
				for k, v := range extra {
					resp.Data[k] = v
				}
			} else {
				resp.Data["token"] = decoded
			}
		}
	}
	now := time.Now()
	c.state.Update(func(s *conceptSession) {
		applySessionMap(s, resp.Data)
		s.LastRefreshTime = now
		s.Cookie = c.buildConceptCookie(s)
	})
	if err := c.state.Persist(); err != nil {
		return "", err
	}
	return "概念版会话续期完成", nil
}

func (c *ConceptAPIClient) FetchAccountStatus(ctx context.Context) (*conceptUserDetailData, *conceptVIPDetailData, error) {
	if c == nil || c.state == nil {
		return nil, nil, fmt.Errorf("concept client unavailable")
	}
	state := c.state.Snapshot()
	if strings.TrimSpace(state.Token) == "" || strings.TrimSpace(state.UserID) == "" {
		return nil, nil, fmt.Errorf("概念版会话不完整")
	}
	now := time.Now()
	query, _ := c.defaultQuery(state, now, false)
	query.Set("plat", "1")
	pk, err := conceptRSARawEncryptHex(map[string]any{
		"token":      state.Token,
		"clienttime": now.Unix(),
	}, conceptLitePublicKeyPEM)
	if err != nil {
		return nil, nil, err
	}
	bodyMap := map[string]any{
		"visit_time": now.Unix(),
		"usertype":   1,
		"p":          strings.ToUpper(pk),
		"userid":     parseConceptInt64(state.UserID),
	}
	bodyJSON, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, nil, err
	}
	query.Set("signature", conceptSignatureAndroid(query, string(bodyJSON)))
	var userResp struct {
		conceptBaseResponse
		Data map[string]any `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodPost, kugouConceptGatewayBaseURL+"/v3/get_my_info?"+query.Encode(), bytes.NewReader(bodyJSON), state, map[string]string{
		"Content-Type": "application/json",
		"x-router":     kugouConceptRouteUser,
	}, &userResp); err != nil {
		return nil, nil, err
	}
	vipQuery, _ := c.defaultQuery(state, time.Now(), false)
	vipQuery.Set("busi_type", "concept")
	vipQuery.Set("signature", conceptSignatureAndroid(vipQuery, ""))
	var vipResp struct {
		conceptBaseResponse
		Data map[string]any `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, kugouConceptVIPBaseURL+"/v1/get_union_vip?"+vipQuery.Encode(), nil, state, nil, &vipResp); err != nil {
		return nil, nil, err
	}
	user := &conceptUserDetailData{Raw: userResp.Data}
	if nickname := firstNonEmpty(valueString(userResp.Data["nickname"]), valueString(userResp.Data["user_name"])); nickname != "" {
		user.Nickname = nickname
	}
	vip := &conceptVIPDetailData{Raw: vipResp.Data}
	expireAt := firstNonEmpty(
		valueString(vipResp.Data["vip_expire_time"]),
		valueString(vipResp.Data["expire_time"]),
		valueString(vipResp.Data["end_time"]),
	)
	vipType := firstNonEmpty(valueString(vipResp.Data["vip_type"]), valueString(vipResp.Data["product_type"]), state.VIPType)
	vipToken := firstNonEmpty(valueString(vipResp.Data["vip_token"]), state.VIPToken)
	c.state.Update(func(s *conceptSession) {
		if user.Nickname != "" {
			s.Nickname = user.Nickname
		}
		if vipType != "" {
			s.VIPType = vipType
		}
		if vipToken != "" {
			s.VIPToken = vipToken
		}
		if expireAt != "" {
			s.VIPExpireTime = expireAt
		}
		s.LastCheckTime = time.Now()
		s.Cookie = c.buildConceptCookie(s)
	})
	_ = c.state.Persist()
	return user, vip, nil
}

func (c *ConceptAPIClient) SignIn(ctx context.Context) (string, error) {
	if c == nil || c.state == nil {
		return "", fmt.Errorf("concept client unavailable")
	}
	state := c.state.Snapshot()
	if strings.TrimSpace(state.Token) == "" || strings.TrimSpace(state.UserID) == "" {
		return "", fmt.Errorf("概念版会话不完整")
	}
	day := time.Now().Format("2006-01-02")
	claimQuery, _ := c.defaultQuery(state, time.Now(), false)
	claimQuery.Set("source_id", "90139")
	claimQuery.Set("receive_day", day)
	claimQuery.Set("signature", conceptSignatureAndroid(claimQuery, ""))
	claimResp, err := c.doBaseResponse(ctx, http.MethodPost, kugouConceptGatewayBaseURL+"/youth/v1/recharge/receive_vip_listen_song?"+claimQuery.Encode(), nil, state, nil)
	if err != nil {
		return "", err
	}
	upgradeQuery, _ := c.defaultQuery(state, time.Now(), false)
	upgradeQuery.Set("kugouid", state.UserID)
	upgradeQuery.Set("ad_type", "1")
	upgradeQuery.Set("signature", conceptSignatureAndroid(upgradeQuery, ""))
	upgradeResp, _ := c.doBaseResponse(ctx, http.MethodPost, kugouConceptGatewayBaseURL+"/youth/v1/listen_song/upgrade_vip_reward?"+upgradeQuery.Encode(), nil, state, nil)
	c.state.Update(func(s *conceptSession) {
		now := time.Now()
		s.LastSignTime = now
		s.LastVIPClaimTime = now
		s.Cookie = c.buildConceptCookie(s)
	})
	_ = c.state.Persist()
	parts := []string{"概念版签到/VIP 领取已尝试"}
	if claimResp != nil {
		parts = append(parts, "领取: "+describeConceptActionResult(claimResp, "领取成功"))
	}
	if upgradeResp != nil {
		parts = append(parts, "升级: "+describeConceptActionResult(upgradeResp, "升级成功"))
	}
	if _, _, statusErr := c.FetchAccountStatus(ctx); statusErr == nil {
		updated := c.state.Snapshot()
		if strings.TrimSpace(updated.VIPType) != "" {
			parts = append(parts, "当前VIP类型: "+updated.VIPType)
		}
		if strings.TrimSpace(updated.VIPExpireTime) != "" {
			parts = append(parts, "VIP到期: "+updated.VIPExpireTime)
		}
	}
	return strings.Join(parts, "\n"), nil
}

func (c *ConceptAPIClient) FetchSongURL(ctx context.Context, song *model.Song, plan kugouDownloadPlan) (*conceptSongURLResponse, error) {
	if c == nil || c.state == nil {
		return nil, fmt.Errorf("concept client unavailable")
	}
	state := c.state.Snapshot()
	if strings.TrimSpace(state.Token) == "" || strings.TrimSpace(state.UserID) == "" {
		return nil, fmt.Errorf("概念版会话不完整")
	}
	extra := ensureSongExtra(song)
	query, _ := c.defaultQuery(state, time.Now(), false)
	query.Set("album_id", firstNonEmpty(extra["album_id"], song.AlbumID, "0"))
	query.Set("area_code", "1")
	query.Set("hash", strings.ToLower(strings.TrimSpace(plan.Hash)))
	query.Set("ssa_flag", "is_fromtrack")
	query.Set("version", "11436")
	query.Set("page_id", kugouConceptPageID)
	query.Set("quality", conceptQualityCode(plan.Quality))
	query.Set("album_audio_id", firstNonEmpty(extra["album_audio_id"], "0"))
	query.Set("behavior", "play")
	query.Set("pid", "411")
	query.Set("cmd", "26")
	query.Set("pidversion", "3001")
	query.Set("IsFreePart", "0")
	query.Set("ppage_id", kugouConceptPPageID)
	query.Set("cdnBackup", "1")
	query.Set("kcard", "0")
	query.Set("module", "")
	query.Set("key", conceptSignKey(strings.ToLower(strings.TrimSpace(plan.Hash)), query.Get("mid"), query.Get("userid"), query.Get("appid")))
	query.Set("signature", conceptSignatureAndroid(query, ""))
	var resp conceptSongURLResponse
	if err := c.doJSON(ctx, http.MethodGet, kugouConceptGatewayBaseURL+"/v5/url?"+query.Encode(), nil, state, map[string]string{"x-router": kugouConceptRouteTrack}, &resp); err != nil {
		return nil, err
	}
	if resp.Status != 1 || len(resp.URL) == 0 || strings.TrimSpace(resp.URL[0]) == "" {
		return nil, fmt.Errorf("概念版 song/url 无可用链接, status=%d err=%s", resp.Status, firstNonEmpty(resp.Error, strconv.Itoa(resp.ErrCode)))
	}
	return &resp, nil
}

func (c *ConceptAPIClient) FetchSongURLNew(ctx context.Context, song *model.Song, plan kugouDownloadPlan) (*conceptSongURLNewResponse, error) {
	if c == nil || c.state == nil {
		return nil, fmt.Errorf("concept client unavailable")
	}
	state := c.state.Snapshot()
	if strings.TrimSpace(state.Token) == "" || strings.TrimSpace(state.UserID) == "" {
		return nil, fmt.Errorf("概念版会话不完整")
	}
	extra := ensureSongExtra(song)
	query, _ := c.defaultQuery(state, time.Now(), false)
	bodyMap := map[string]any{
		"area_code": "1",
		"behavior":  "play",
		"qualities": []string{"128", "320", "flac", "high", "multitrack", "viper_atmos", "viper_tape", "viper_clear"},
		"resource": map[string]any{
			"album_audio_id":  firstNonEmpty(extra["album_audio_id"], "0"),
			"collect_list_id": "3",
			"collect_time":    time.Now().UnixMilli(),
			"hash":            strings.ToLower(strings.TrimSpace(plan.Hash)),
			"id":              0,
			"page_id":         1,
			"type":            "audio",
		},
		"token": state.Token,
		"tracker_param": map[string]any{
			"all_m":         1,
			"auth":          "",
			"is_free_part":  0,
			"key":           conceptSignKey(strings.ToLower(strings.TrimSpace(plan.Hash)), state.Device.Mid, state.UserID, kugouConceptAppID),
			"module_id":     0,
			"need_climax":   1,
			"need_xcdn":     1,
			"open_time":     "",
			"pid":           "411",
			"pidversion":    "3001",
			"priv_vip_type": firstNonEmpty(state.VIPType, defaultConceptVIPType),
			"viptoken":      state.VIPToken,
		},
		"userid": state.UserID,
		"vip":    parseConceptInt64(firstNonEmpty(state.VIPType, "0")),
	}
	bodyJSON, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, err
	}
	query.Set("signature", conceptSignatureAndroid(query, string(bodyJSON)))
	var raw json.RawMessage
	if err := c.doJSON(ctx, http.MethodPost, "http://tracker.kugou.com/v6/priv_url?"+query.Encode(), bytes.NewReader(bodyJSON), state, map[string]string{"Content-Type": "application/json"}, &raw); err != nil {
		return nil, err
	}
	resp := &conceptSongURLNewResponse{Raw: raw}
	var env struct {
		Status  int             `json:"status"`
		ErrCode int             `json:"errcode,omitempty"`
		Error   string          `json:"error,omitempty"`
		Data    json.RawMessage `json:"data,omitempty"`
	}
	if err := json.Unmarshal(raw, &env); err == nil {
		resp.Status = env.Status
		resp.ErrCode = env.ErrCode
		resp.Error = env.Error
		resp.Data = env.Data
	}
	return resp, nil
}

func (c *ConceptAPIClient) ensureDevice(ctx context.Context) (conceptDeviceInfo, error) {
	if c == nil || c.state == nil {
		return conceptDeviceInfo{}, fmt.Errorf("concept client unavailable")
	}
	state := c.state.Snapshot()
	if strings.TrimSpace(state.Device.Dfid) != "" && strings.TrimSpace(state.Device.Guid) != "" && strings.TrimSpace(state.Device.Mid) != "" && strings.TrimSpace(state.Device.Dev) != "" && strings.TrimSpace(state.Device.Mac) != "" {
		return state.Device, nil
	}
	device := normalizeConceptDevice(state.Device)
	query, _ := c.defaultQueryForDevice(device, time.Now())
	bodyCipher, bodyKey, err := conceptPlaylistAesEncrypt(map[string]any{
		"availableRamSize":   int64(4983533568),
		"availableRomSize":   int64(48114719),
		"availableSDSize":    int64(48114717),
		"basebandVer":        "",
		"batteryLevel":       100,
		"batteryStatus":      3,
		"brand":              "Redmi",
		"buildSerial":        "unknown",
		"device":             "marble",
		"imei":               device.Guid,
		"imsi":               "",
		"manufacturer":       "Xiaomi",
		"uuid":               device.Guid,
		"accelerometer":      false,
		"accelerometerValue": "",
		"gravity":            false,
		"gravityValue":       "",
		"gyroscope":          false,
		"gyroscopeValue":     "",
		"light":              false,
		"lightValue":         "",
		"magnetic":           false,
		"magneticValue":      "",
		"orientation":        false,
		"orientationValue":   "",
		"pressure":           false,
		"pressureValue":      "",
		"step_counter":       false,
		"step_counterValue":  "",
		"temperature":        false,
		"temperatureValue":   "",
	})
	if err != nil {
		return device, err
	}
	p, err := conceptRSAPKCS1v15EncryptHex(map[string]any{
		"aes":   bodyKey,
		"uid":   parseConceptInt64(state.UserID),
		"token": strings.TrimSpace(state.Token),
	}, conceptLitePublicKeyPEM)
	if err != nil {
		return device, err
	}
	query.Set("part", "1")
	query.Set("platid", "1")
	query.Set("p", p)
	query.Set("signature", conceptSignatureAndroid(query, bodyCipher))
	bodyBytes, err := c.doBytes(ctx, http.MethodPost, kugouConceptUserServiceBaseURL+"/risk/v2/r_register_dev?"+query.Encode(), strings.NewReader(bodyCipher), state, map[string]string{"Content-Type": "text/plain;charset=UTF-8"})
	if err != nil {
		device.Dfid = firstNonEmpty(device.Dfid, conceptRandomAlphaNum(24))
		device.Source = "fallback"
		device.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		c.state.Update(func(s *conceptSession) {
			s.Device = device
			s.Cookie = c.buildConceptCookie(s)
		})
		_ = c.state.Persist()
		return device, nil
	}
	decodedText, err := conceptPlaylistAesDecryptFromBinary(bodyBytes, bodyKey)
	if err != nil {
		decodedText = strings.TrimSpace(string(bodyBytes))
	}
	var resp struct {
		Status int `json:"status"`
		Data   struct {
			Dfid string `json:"dfid"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(decodedText), &resp); err != nil {
		return device, err
	}
	device.Dfid = firstNonEmpty(strings.TrimSpace(resp.Data.Dfid), device.Dfid, conceptRandomAlphaNum(24))
	device.Source = "register/dev"
	device.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	c.state.Update(func(s *conceptSession) {
		s.Device = device
		s.Cookie = c.buildConceptCookie(s)
	})
	_ = c.state.Persist()
	return device, nil
}

func (c *ConceptAPIClient) doJSON(ctx context.Context, method, rawURL string, body io.Reader, state conceptSession, headers map[string]string, out any) error {
	bodyBytes, err := c.doBytes(ctx, method, rawURL, body, state, headers)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(bodyBytes, out); err != nil {
		return err
	}
	return nil
}

func (c *ConceptAPIClient) doBaseResponse(ctx context.Context, method, rawURL string, body io.Reader, state conceptSession, headers map[string]string) (*conceptBaseResponse, error) {
	bodyBytes, err := c.doBytes(ctx, method, rawURL, body, state, headers)
	if err != nil {
		return nil, err
	}
	var resp conceptBaseResponse
	if err := json.Unmarshal(bodyBytes, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *ConceptAPIClient) doBytes(ctx context.Context, method, rawURL string, body io.Reader, state conceptSession, headers map[string]string) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("concept client unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", kugouConceptUserAgent)
	req.Header.Set("kg-rc", "1")
	req.Header.Set("kg-thash", kugouConceptKGTHash)
	req.Header.Set("kg-rec", "1")
	req.Header.Set("kg-rf", kugouConceptKGRF)
	if state.Device.Dfid != "" {
		req.Header.Set("dfid", state.Device.Dfid)
	}
	if state.Device.Mid != "" {
		req.Header.Set("mid", state.Device.Mid)
	}
	for key, value := range headers {
		if strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}
	if cookie := c.buildConceptCookie(&state); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("concept api http %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}
	return bodyBytes, nil
}

func (c *ConceptAPIClient) defaultQuery(state conceptSession, now time.Time, includeAuth bool) (url.Values, conceptDeviceInfo) {
	device := normalizeConceptDevice(state.Device)
	query := url.Values{}
	query.Set("dfid", firstNonEmpty(device.Dfid, "-"))
	query.Set("mid", device.Mid)
	query.Set("uuid", "-")
	query.Set("appid", kugouConceptAppID)
	query.Set("clientver", kugouConceptClientVer)
	query.Set("clienttime", strconv.FormatInt(now.Unix(), 10))
	if includeAuth || strings.TrimSpace(state.Token) != "" || strings.TrimSpace(state.UserID) != "" {
		if strings.TrimSpace(state.Token) != "" {
			query.Set("token", strings.TrimSpace(state.Token))
		}
		if strings.TrimSpace(state.UserID) != "" {
			query.Set("userid", strings.TrimSpace(state.UserID))
		}
	}
	return query, device
}

func (c *ConceptAPIClient) defaultQueryForDevice(device conceptDeviceInfo, now time.Time) (url.Values, conceptDeviceInfo) {
	device = normalizeConceptDevice(device)
	query := url.Values{}
	query.Set("dfid", firstNonEmpty(device.Dfid, "-"))
	query.Set("mid", device.Mid)
	query.Set("uuid", "-")
	query.Set("appid", kugouConceptAppID)
	query.Set("clientver", kugouConceptClientVer)
	query.Set("clienttime", strconv.FormatInt(now.Unix(), 10))
	return query, device
}

func (c *ConceptAPIClient) buildConceptCookie(state *conceptSession) string {
	if state == nil {
		return ""
	}
	device := normalizeConceptDevice(state.Device)
	pairs := []string{}
	appendKV := func(key, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		pairs = append(pairs, key+"="+strings.TrimSpace(value))
	}
	appendKV("token", state.Token)
	appendKV("userid", state.UserID)
	appendKV("t1", state.T1)
	appendKV("vip_token", state.VIPToken)
	appendKV("vip_type", firstNonEmpty(state.VIPType, defaultConceptVIPType))
	appendKV("dfid", device.Dfid)
	appendKV("KUGOU_API_MID", device.Mid)
	appendKV("KUGOU_API_GUID", device.Guid)
	appendKV("KUGOU_API_DEV", device.Dev)
	appendKV("KUGOU_API_MAC", device.Mac)
	if len(pairs) == 0 {
		return strings.TrimSpace(state.Cookie)
	}
	return strings.Join(pairs, "; ")
}

func applySessionMap(state *conceptSession, values map[string]any) {
	if state == nil || values == nil {
		return
	}
	if token := valueString(values["token"]); token != "" {
		state.Token = token
	}
	if userID := firstNonEmpty(valueString(values["userid"]), valueString(values["user_id"])); userID != "" {
		state.UserID = userID
	}
	if t1 := valueString(values["t1"]); t1 != "" {
		state.T1 = t1
	}
	if vipType := valueString(values["vip_type"]); vipType != "" {
		state.VIPType = vipType
	}
	if vipToken := valueString(values["vip_token"]); vipToken != "" {
		state.VIPToken = vipToken
	}
	if nickname := valueString(values["nickname"]); nickname != "" {
		state.Nickname = nickname
	}
	state.Device = normalizeConceptDevice(state.Device)
}

func valueString(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case json.Number:
		return strings.TrimSpace(x.String())
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	default:
		return ""
	}
}

func buildConceptQRCodeURL(key string) string {
	return "https://h5.kugou.com/apps/loginQRCode/html/index.html?qrcode=" + url.QueryEscape(strings.TrimSpace(key))
}

func parseConceptInt64(value string) int64 {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func normalizeConceptDevice(device conceptDeviceInfo) conceptDeviceInfo {
	if strings.TrimSpace(device.Guid) == "" {
		device.Guid = conceptMD5(conceptRandomGUID())
	}
	if strings.TrimSpace(device.Mid) == "" {
		device.Mid = conceptCalculateMid(device.Guid)
	}
	if strings.TrimSpace(device.Dev) == "" {
		device.Dev = conceptRandomAlpha(10)
	}
	device.Dev = strings.ToUpper(device.Dev)
	if strings.TrimSpace(device.Mac) == "" {
		device.Mac = "02:00:00:00:00:00"
	}
	if strings.TrimSpace(device.Dfid) == "" {
		device.Dfid = "-"
	}
	return device
}

func describeConceptActionResult(resp *conceptBaseResponse, successText string) string {
	if resp == nil {
		return "未返回结果"
	}
	message := strings.TrimSpace(firstNonEmpty(resp.Message, resp.Error))
	if resp.Status == 1 {
		if message != "" {
			return message
		}
		return successText
	}
	if message != "" {
		return message
	}
	if resp.ErrCode != 0 {
		return fmt.Sprintf("失败(errcode=%d)", resp.ErrCode)
	}
	if resp.Code != 0 {
		return fmt.Sprintf("失败(code=%d)", resp.Code)
	}
	return fmt.Sprintf("失败(status=%d)", resp.Status)
}

func conceptQualityCode(q platform.Quality) string {
	switch q {
	case platform.QualityHiRes:
		return "high"
	case platform.QualityLossless:
		return "flac"
	case platform.QualityHigh:
		return "320"
	default:
		return "128"
	}
}
