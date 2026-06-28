package netease

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"
)

func apiRequest(ctx context.Context, eapiOption EAPIOption, options RequestData) (string, http.Header, error) {
	data := spliceStr(eapiOption.Path, eapiOption.Json)
	return createNewRequest(ctx, formatToParams(data), eapiOption.Url, options)
}

func spliceStr(path string, data string) string {
	nobodyKnowThis := "36cd479b6b5"
	text := fmt.Sprintf("nobody%suse%smd5forencrypt", path, data)
	md5sum := md5.Sum([]byte(text))
	md5str := fmt.Sprintf("%x", md5sum)
	return fmt.Sprintf("%s-%s-%s-%s-%s", path, nobodyKnowThis, data, nobodyKnowThis, md5str)
}

func formatToParams(str string) string {
	return fmt.Sprintf("params=%X", eapiEncrypt(str))
}

func chooseUserAgent() string {
	userAgentList := []string{
		"Mozilla/5.0 (iPhone; CPU iPhone OS 9_1 like Mac OS X) AppleWebKit/601.1.46 (KHTML, like Gecko) Version/9.0 Mobile/13B143 Safari/601.1",
		"Mozilla/5.0 (Linux; Android 5.0; SM-G900P Build/LRX21T) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/59.0.3071.115 Mobile Safari/537.36",
		"Mozilla/5.0 (Linux; Android 6.0; Nexus 5 Build/MRA58N) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/59.0.3071.115 Mobile Safari/537.36",
		"Mozilla/5.0 (Linux; Android 5.1.1; Nexus 6 Build/LYZ28E) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/59.0.3071.115 Mobile Safari/537.36",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 10_3_2 like Mac OS X) AppleWebKit/603.2.4 (KHTML, like Gecko) Mobile/14F89;GameHelper",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 10_0 like Mac OS X) AppleWebKit/602.1.38 (KHTML, like Gecko) Version/10.0 Mobile/14A300 Safari/602.1",
		"NeteaseMusic/6.5.0.1575377963(164);Dalvik/2.1.0 (Linux; U; Android 9; MIX 2 MIUI/V12.0.1.0.PDECNXM)",
	}
	return userAgentList[rand.Intn(len(userAgentList))]
}

func encodeURIComponent(str string) string {
	r := neturl.QueryEscape(str)
	return strings.ReplaceAll(r, "+", "%20")
}

// randomNMTID generates a NetEase NMTID device cookie. The real client value is
// a "00" prefix followed by ~30 alphanumeric chars; the anti-cheat only checks
// for presence/shape, not server-issued validity, so a random one is accepted.
func randomNMTID() string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 30)
	for i := range b {
		b[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return "00" + string(b)
}

func createNewRequest(ctx context.Context, data string, endpoint string, options RequestData) (string, http.Header, error) {
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(data))
	if err != nil {
		return "", nil, err
	}

	cookie := map[string]interface{}{}
	for _, v := range options.Cookies {
		cookie[v.Name] = v.Value
	}
	for _, v := range options.Headers {
		req.Header.Set(v.Name, v.Value)
	}

	cookie["appver"] = "8.9.70"
	cookie["buildver"] = strconv.FormatInt(time.Now().Unix(), 10)[:10]
	cookie["resolution"] = "1920x1080"
	cookie["os"] = "android"
	// NMTID is a device-tracking cookie that NetEase's anti-cheat requires on the
	// stream/url endpoints. When it is absent, those endpoints reject the request
	// with code -462 ("检测到您的网络环境存在风险") / verifyType:50 regardless of a
	// valid MUSIC_U or the source IP. Inject a random one when the caller didn't
	// supply it so a bare MUSIC_U cookie still works.
	if _, ok := cookie["NMTID"]; !ok {
		cookie["NMTID"] = randomNMTID()
	}
	if _, ok := cookie["MUSIC_U"]; !ok {
		if _, ok := cookie["MUSIC_A"]; !ok {
			cookie["MUSIC_A"] = "4ee5f776c9ed1e4d5f031b09e084c6cb333e43ee4a841afeebbef9bbf4b7e4152b51ff20ecb9e8ee9e89ab23044cf50d1609e4781e805e73a138419e5583bc7fd1e5933c52368d9127ba9ce4e2f233bf5a77ba40ea6045ae1fc612ead95d7b0e0edf70a74334194e1a190979f5fc12e9968c3666a981495b33a649814e309366"
		}
	}

	var cookies string
	for key, val := range cookie {
		cookies += encodeURIComponent(key) + "=" + encodeURIComponent(fmt.Sprintf("%v", val)) + "; "
	}
	req.Header.Set("Cookie", strings.TrimRight(cookies, "; "))
	if len(req.Header["Content-Type"]) == 0 {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("User-Agent", chooseUserAgent())

	resp, err := client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, err
	}
	return string(body), resp.Header, nil
}
