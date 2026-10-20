package proxy

import (
	"net/http"
	"strings"
	"testing"
)

func respWith(headers map[string]string) *http.Response {
	h := http.Header{}
	for k, v := range headers {
		h.Set(k, v)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: h}
}

func TestIsMirrorChallenge_RealSample(t *testing.T) {
	// 实测抓到的中文版 Cloudflare 挑战页开头（镜像站返回）。
	body := []byte(`<!DOCTYPE html><html lang="zh-CN" class="overflow-x-clip"> <head><meta charset="UTF-8">` +
		`<script>function a0_0xc5c6(_0x2ed93b,_0x524a98){var _0x58096d=a0_0xab7e();` +
		`_0x2ed93b=_0x2ed93b-(-0x1d6+-0x755+0xd*0xbe);return _0x58096d[_0x2ed93b];}</script>`)
	resp := respWith(map[string]string{
		"Server":       "cloudflare",
		"Cf-Ray":       "a4d15a7208694aba-LAX",
		"Content-Type": "text/html; charset=utf-8",
	})
	if !isMirrorChallenge(resp, body) {
		t.Fatal("实测挑战页样本必须被识别")
	}
}

func TestIsMirrorChallenge_Signatures(t *testing.T) {
	cases := []string{
		`<html><body><script src="/cdn-cgi/challenge-platform/h/b/orchestrate/jsch/v1"></script></body></html>`,
		`<html><body>Just a moment...</body></html>`,
		`<html><body>Attention Required! | Cloudflare</body></html>`,
		`<html><script>window._cf_chl_opt={cvId:3}</script></html>`,
	}
	for _, body := range cases {
		resp := respWith(map[string]string{"Server": "cloudflare", "Content-Type": "text/html"})
		if !isMirrorChallenge(resp, []byte(body)) {
			t.Errorf("应识别为挑战页: %s", body)
		}
	}
}

func TestIsMirrorChallenge_MitigatedHeader(t *testing.T) {
	resp := respWith(map[string]string{"Cf-Mitigated": "challenge"})
	if !isMirrorChallenge(resp, []byte("anything")) {
		t.Fatal("Cf-Mitigated 头必须直接判定为挑战")
	}
}

func TestIsMirrorChallenge_NormalPassthrough(t *testing.T) {
	cases := []struct {
		headers map[string]string
		body    string
		desc    string
	}{
		{
			map[string]string{"Server": "cloudflare", "Content-Type": "application/zip"},
			"PK\x03\x04binary",
			"非 HTML 的 CF 响应（大文件）不应误判",
		},
		{
			map[string]string{"Server": "github.com", "Content-Type": "text/html"},
			"<html><body>real github page</body></html>",
			"无 CF 头的正常页面不应误判",
		},
		{
			map[string]string{"Server": "cloudflare", "Content-Type": "text/html"},
			"<html><body>normal cached page</body></html>",
			"CF 但其内容为正常页面时不应误判",
		},
		{
			map[string]string{"Content-Type": "application/json"},
			`{"message":"ok"}`,
			"JSON 不应误判",
		},
	}
	for _, c := range cases {
		resp := respWith(c.headers)
		if isMirrorChallenge(resp, []byte(c.body)) {
			t.Errorf("误判为挑战页: %s", c.desc)
		}
	}
}

func TestIsMirrorChallenge_LongBodyTruncation(t *testing.T) {
	// 挑战特征出现在 64KB 之后不应被当作挑战（避免正常大文件误伤）。
	body := []byte(strings.Repeat("a", 80<<10) + "challenge-platform")
	resp := respWith(map[string]string{"Server": "cloudflare", "Content-Type": "text/html"})
	if isMirrorChallenge(resp, body) {
		t.Fatal("超过截断窗口的特征不应命中")
	}
}
