package netease

import (
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

func apiRequest(eapiOption EAPIOption, options RequestData) (string, http.Header, error) {
	data := spliceStr(eapiOption.Path, eapiOption.Json)
	return createNewRequest(formatToParams(data), eapiOption.Url, options)
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
	rand.Seed(time.Now().UnixNano())
	return userAgentList[rand.Intn(len(userAgentList))]
}

func encodeURIComponent(str string) string {
	r := neturl.QueryEscape(str)
	return strings.ReplaceAll(r, "+", "%20")
}

func createNewRequest(data string, endpoint string, options RequestData) (string, http.Header, error) {
	client := options.Client
	if client == nil {
		client = &http.Client{}
	}
	req, err := http.NewRequest("POST", endpoint, strings.NewReader(data))
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
