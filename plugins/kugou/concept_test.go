package kugou

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/guohuiyuan/music-lib/model"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

func TestConceptSessionPersistAndStatus(t *testing.T) {
	var persisted map[string]string
	mgr := NewConceptSessionManager(nil, func(pairs map[string]string) error {
		persisted = pairs
		return nil
	}, conceptSession{Enabled: true, Token: "tok", UserID: "uid", Nickname: "tester", AutoRefresh: true})
	if !mgr.HasUsableSession() {
		t.Fatal("expected usable session")
	}
	if err := mgr.Persist(); err != nil {
		t.Fatalf("Persist() error = %v", err)
	}
	if persisted["concept_token"] != "tok" || persisted["concept_user_id"] != "uid" {
		t.Fatalf("persisted session mismatch: %#v", persisted)
	}
	status := mgr.StatusSummary()
	if !strings.Contains(status, "tester") || !strings.Contains(status, "uid") {
		t.Fatalf("StatusSummary()=%q", status)
	}
}

func TestConceptCreateQRCodeAndCheck(t *testing.T) {
	oldClient := http.DefaultClient
	defer func() { http.DefaultClient = oldClient }()
	hits := map[string]int{}
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		hits[req.URL.Path]++
		var body string
		switch req.URL.Path {
		case "/risk/v2/r_register_dev":
			body = `{"status":1,"data":{"dfid":"df123"}}`
		case "/v2/qrcode":
			body = `{"status":1,"data":{"qrcode":"qr-key-1"}}`
		case "/v2/get_userinfo_qrcode":
			body = `{"status":1,"data":{"status":4,"token":"token-1","userid":12345,"nickname":"nick"}}`
		default:
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	mgr := NewConceptSessionManager(nil, nil, conceptSession{Enabled: true})
	data, err := mgr.CreateQRCode(context.Background())
	if err != nil {
		t.Fatalf("CreateQRCode() error = %v", err)
	}
	if data.URL != "https://h5.kugou.com/apps/loginQRCode/html/index.html?qrcode=qr-key-1" {
		t.Fatalf("CreateQRCode URL=%q", data.URL)
	}
	if !strings.HasPrefix(data.Base64, "data:image/png;base64,") {
		t.Fatalf("CreateQRCode base64=%q", data.Base64)
	}
	check, err := mgr.CheckQRCode(context.Background())
	if err != nil {
		t.Fatalf("CheckQRCode() error = %v", err)
	}
	if check.Status != 4 {
		t.Fatalf("CheckQRCode status=%d", check.Status)
	}
	state := mgr.Snapshot()
	if state.Token != "token-1" || state.UserID != "12345" || state.Device.Dfid != "df123" {
		t.Fatalf("unexpected state: %#v", state)
	}
	if state.QRKey != "" || state.QRURL != "" || state.QRBase64 != "" {
		t.Fatalf("expected qr fields cleared after success, got state=%#v", state)
	}
	if hits["/risk/v2/r_register_dev"] == 0 {
		t.Fatal("expected risk/v2/r_register_dev to be called")
	}
}

func TestBuildQRStatusCaption(t *testing.T) {
	caption := buildQRStatusCaption(conceptQRCheckData{Status: 2, Nickname: conceptJSONText("tester"), UserID: conceptJSONText("12345")})
	for _, want := range []string{"二维码状态: 已扫码，待确认", "昵称: tester", "用户ID: 12345", "已扫码，等待确认"} {
		if !strings.Contains(caption, want) {
			t.Fatalf("caption=%q missing %q", caption, want)
		}
	}
}

func TestConceptFetchSongURLAndClientResolve(t *testing.T) {
	oldClient := http.DefaultClient
	defer func() { http.DefaultClient = oldClient }()
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v5/url" {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":1,"data":{}}`))}, nil
		}
		q := req.URL.Query()
		if q.Get("hash") != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
			t.Fatalf("unexpected hash=%q", q.Get("hash"))
		}
		if q.Get("signature") == "" {
			t.Fatal("expected signature query")
		}
		body := `{"status":1,"url":["https://concept.cdn/test.flac"],"timeLength":226000,"extName":"flac"}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	mgr := NewConceptSessionManager(nil, nil, conceptSession{Enabled: true, Token: "tok", UserID: "uid", Device: conceptDeviceInfo{Dfid: "dfid"}})
	resp, err := mgr.FetchSongURL(context.Background(), &model.Song{ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", AlbumID: "41668184", Extra: map[string]string{"album_audio_id": "123", "album_id": "41668184"}}, kugouDownloadPlan{Hash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Quality: platform.QualityLossless})
	if err != nil {
		t.Fatalf("FetchSongURL() error = %v", err)
	}
	if resp.URL[0] != "https://concept.cdn/test.flac" {
		t.Fatalf("FetchSongURL() url=%v", resp.URL)
	}
	client := NewClient("", nil)
	client.AttachConcept(mgr)
	resolved, err := client.fetchConceptSongURL(context.Background(), &model.Song{ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", AlbumID: "41668184", Extra: map[string]string{"album_audio_id": "123", "album_id": "41668184"}}, kugouDownloadPlan{Hash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Quality: platform.QualityLossless, Format: "flac"})
	if err != nil {
		t.Fatalf("fetchConceptSongURL() error = %v", err)
	}
	if resolved.URL != "https://concept.cdn/test.flac" || resolved.Ext != "flac" {
		t.Fatalf("resolved song=%+v", resolved)
	}
	if resolved.Extra["concept_source"] != "song/url" {
		t.Fatalf("resolved.Extra[concept_source]=%q", resolved.Extra["concept_source"])
	}
}

func TestApplySessionMap(t *testing.T) {
	state := &conceptSession{}
	var payload map[string]any
	if err := json.Unmarshal([]byte(`{"token":"tok","userid":"uid","t1":"t1","vip_type":6,"vip_token":"vip","nickname":"nick"}`), &payload); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}
	applySessionMap(state, payload)
	if state.Token != "tok" || state.UserID != "uid" || state.T1 != "t1" || state.VIPToken != "vip" || state.Nickname != "nick" {
		t.Fatalf("unexpected state: %#v", state)
	}
	if state.VIPType != "6" {
		t.Fatalf("VIPType=%q want 6", state.VIPType)
	}
}

func TestConceptFetchAccountStatusUpdatesSession(t *testing.T) {
	oldClient := http.DefaultClient
	defer func() { http.DefaultClient = oldClient }()
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v3/get_my_info":
			body := `{"status":1,"data":{"nickname":"tester"}}`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		case "/v1/get_union_vip":
			body := `{"status":1,"data":{"vip_type":"6","vip_token":"vip-token","expire_time":"2099-01-01"}}`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			t.Fatalf("unexpected path: %s", req.URL.Path)
			return nil, nil
		}
	})}
	mgr := NewConceptSessionManager(nil, nil, conceptSession{Enabled: true, Token: "tok", UserID: "123", Device: conceptDeviceInfo{Dfid: "df", Guid: "guid", Mid: "mid", Dev: "DEV1234567", Mac: "02:00:00:00:00:00"}})
	user, vip, err := mgr.FetchAccountStatus(context.Background())
	if err != nil {
		t.Fatalf("FetchAccountStatus() error = %v", err)
	}
	if user == nil || user.Nickname != "tester" {
		t.Fatalf("unexpected user = %#v", user)
	}
	if vip == nil || vip.Raw["vip_token"] != "vip-token" {
		t.Fatalf("unexpected vip = %#v", vip)
	}
	state := mgr.Snapshot()
	if state.Nickname != "tester" || state.VIPType != "6" || state.VIPToken != "vip-token" || state.VIPExpireTime != "2099-01-01" {
		t.Fatalf("unexpected session state = %#v", state)
	}
	if state.LastCheckTime.IsZero() {
		t.Fatal("expected LastCheckTime updated")
	}
}

func TestConceptManualRenewUpdatesSession(t *testing.T) {
	oldClient := http.DefaultClient
	defer func() { http.DefaultClient = oldClient }()
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v5/login_by_token":
			body := `{"status":1,"data":{"token":"tok-new","userid":"321","t1":"t1-new","vip_type":7,"vip_token":"vip-new","nickname":"renewed"}}`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			t.Fatalf("unexpected path: %s", req.URL.Path)
			return nil, nil
		}
	})}
	mgr := NewConceptSessionManager(nil, nil, conceptSession{Enabled: true, Token: "tok-old", UserID: "123", T1: "t1-old", Device: conceptDeviceInfo{Dfid: "df", Guid: "guid", Mid: "mid", Dev: "DEV1234567", Mac: "02:00:00:00:00:00"}})
	msg, err := mgr.ManualRenew(context.Background())
	if err != nil {
		t.Fatalf("ManualRenew() error = %v", err)
	}
	if !strings.Contains(msg, "续期完成") {
		t.Fatalf("ManualRenew() msg=%q", msg)
	}
	state := mgr.Snapshot()
	if state.Token != "tok-new" || state.UserID != "321" || state.T1 != "t1-new" || state.VIPType != "7" || state.VIPToken != "vip-new" || state.Nickname != "renewed" {
		t.Fatalf("unexpected renewed state = %#v", state)
	}
	if state.LastRefreshTime.IsZero() {
		t.Fatal("expected LastRefreshTime updated")
	}
}

func TestConceptFetchSongURLNew(t *testing.T) {
	oldClient := http.DefaultClient
	defer func() { http.DefaultClient = oldClient }()
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v6/priv_url" {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		body := `{"status":1,"data":[{"quality":"flac","tracker_url":"https://concept.cdn/enc.mflac"}]}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	mgr := NewConceptSessionManager(nil, nil, conceptSession{Enabled: true, Token: "tok", UserID: "123", VIPType: "6", VIPToken: "vip", Device: conceptDeviceInfo{Dfid: "df", Guid: "guid", Mid: "mid", Dev: "DEV1234567", Mac: "02:00:00:00:00:00"}})
	resp, err := mgr.FetchSongURLNew(context.Background(), &model.Song{ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Extra: map[string]string{"album_audio_id": "123"}}, kugouDownloadPlan{Hash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Quality: platform.QualityLossless})
	if err != nil {
		t.Fatalf("FetchSongURLNew() error = %v", err)
	}
	if resp.Status != 1 {
		t.Fatalf("FetchSongURLNew status=%d", resp.Status)
	}
	if !strings.Contains(string(resp.Data), "tracker_url") {
		t.Fatalf("FetchSongURLNew data=%s", string(resp.Data))
	}
}

func TestResolveConceptSongURLNewUsesNonEncryptedTrackerURL(t *testing.T) {
	client := NewClient("", nil)
	resp := &conceptSongURLNewResponse{
		Status: 1,
		Data: json.RawMessage(`[
			{"quality":"flac","tracker_url":"https://concept.cdn/encrypted.mflac","extname":"mflac"},
			{"quality":"320","tracker_url":"https://concept.cdn/plain.mp3","extname":"mp3"}
		]`),
	}
	resolved, ok := client.resolveConceptSongURLNew(&model.Song{ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Extra: map[string]string{}}, kugouDownloadPlan{Hash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Quality: platform.QualityHigh, Format: "mp3"}, resp)
	if !ok || resolved == nil {
		t.Fatal("expected resolveConceptSongURLNew to resolve a usable tracker url")
	}
	if resolved.URL != "https://concept.cdn/plain.mp3" {
		t.Fatalf("resolved.URL=%q", resolved.URL)
	}
	if resolved.Ext != "mp3" {
		t.Fatalf("resolved.Ext=%q", resolved.Ext)
	}
	if resolved.Extra["concept_source"] != "song/url/new" {
		t.Fatalf("concept_source=%q", resolved.Extra["concept_source"])
	}
}

func TestConceptSongURLNewAuthError(t *testing.T) {
	err := conceptSongURLNewAuthError(&conceptSongURLNewResponse{ErrCode: 20018})
	if err == nil || !strings.Contains(err.Error(), "auth") {
		t.Fatalf("expected auth error for errcode 20018, got %v", err)
	}
	err = conceptSongURLNewAuthError(&conceptSongURLNewResponse{Error: "需要VIP"})
	if err == nil || !strings.Contains(err.Error(), "auth") {
		t.Fatalf("expected auth error for vip message, got %v", err)
	}
}

func TestConceptStatusSummaryIncludesMoreFields(t *testing.T) {
	mgr := NewConceptSessionManager(nil, nil, conceptSession{
		Enabled:       true,
		Token:         "tok",
		UserID:        "uid",
		T1:            "t1",
		Nickname:      "tester",
		VIPType:       "6",
		VIPExpireTime: "2099-01-01",
		SessionSource: "concept_qr",
		Device: conceptDeviceInfo{
			Dfid: "dfid",
			Mid:  "mid",
			Dev:  "DEV1234567",
		},
	})
	summary := mgr.StatusSummary()
	for _, want := range []string{"酷狗概念版状态", "- 会话: 可用", "- Token: 已存在", "- T1: 已存在", "- VIP到期: 2099-01-01", "DFID: dfid", "MID: mid", "DEV: DEV1234567", "- 来源: concept_qr"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("StatusSummary()=%q missing %q", summary, want)
		}
	}
}

func TestDescribeQRStatus(t *testing.T) {
	tests := map[int]string{
		0: "已过期",
		1: "等待扫码",
		2: "已扫码，待确认",
		4: "登录成功",
		9: "9",
	}
	for input, want := range tests {
		if got := describeQRStatus(input); got != want {
			t.Fatalf("describeQRStatus(%d)=%q want %q", input, got, want)
		}
	}
}

func TestConceptSignInAcceptsStringData(t *testing.T) {
	oldClient := http.DefaultClient
	defer func() { http.DefaultClient = oldClient }()
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v3/get_my_info":
			body := `{"status":1,"data":{"nickname":"tester"}}`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		case "/v1/get_union_vip":
			body := `{"status":1,"data":{"vip_type":"6","vip_token":"vip-token","expire_time":"2099-01-01"}}`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		case "/youth/v1/recharge/receive_vip_listen_song":
			body := `{"status":1,"message":"领取成功","data":"ok"}`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		case "/youth/v1/listen_song/upgrade_vip_reward":
			body := `{"status":1,"message":"升级成功","data":"ok"}`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			t.Fatalf("unexpected path: %s", req.URL.Path)
			return nil, nil
		}
	})}
	mgr := NewConceptSessionManager(nil, nil, conceptSession{Enabled: true, Token: "tok", UserID: "123", Device: conceptDeviceInfo{Dfid: "df", Guid: "guid", Mid: "mid", Dev: "DEV1234567", Mac: "02:00:00:00:00:00"}})
	msg, err := mgr.SignIn(context.Background())
	if err != nil {
		t.Fatalf("SignIn() error = %v", err)
	}
	for _, want := range []string{"概念版签到/VIP 领取已尝试", "领取: 领取成功", "升级: 升级成功", "当前VIP类型: 6", "VIP到期: 2099-01-01"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("SignIn()=%q missing %q", msg, want)
		}
	}
}
