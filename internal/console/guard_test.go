package console

import (
	"net/http"
	"strings"
	"testing"
)

// AC-2：/api 请求没有 X-Airlock-Token 或令牌错误时，返回 401，不调用依赖。
func TestTokenGuard(t *testing.T) {
	e := newEnv(t)
	wrong := map[string]string{
		"没有令牌":    "",
		"等长的错误令牌": "fake-token-1234567891",
		"明显错误的令牌": "wrong",
	}
	for name, token := range wrong {
		resp := e.request(t, http.MethodGet, "/api/tiers", "", token, "", "")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s：状态码 = %d，want %d", name, resp.StatusCode, http.StatusUnauthorized)
		}
		body(t, resp)
	}
	if got := e.svc.recorded(); len(got) != 0 {
		t.Errorf("依赖被调用了 %d 次，want 0：%q", len(got), got)
	}
	if got := e.keys.recorded(); len(got) != 0 {
		t.Errorf("Keys 被调用了 %d 次，want 0：%q", len(got), got)
	}
	if got := e.adv.recorded(); len(got) != 0 {
		t.Errorf("Advisor 被调用了 %d 次，want 0：%q", len(got), got)
	}

	// 静态页面不需要令牌。
	resp := e.request(t, http.MethodGet, "/", "", "", "", "")
	if resp.StatusCode == http.StatusUnauthorized {
		t.Errorf("静态页面被要求令牌：状态码 = %d", resp.StatusCode)
	}
	body(t, resp)

	// 正确的令牌通过检查。
	resp = e.request(t, http.MethodGet, "/api/tiers", "", fakeToken, "", "")
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		t.Errorf("正确的令牌没有通过检查：状态码 = %d", resp.StatusCode)
	}
	body(t, resp)
}

// 每个响应都带防点击劫持和防嗅探的头（docs/security.md 的 T-12）。
func TestSecurityHeaders(t *testing.T) {
	e := newEnv(t)
	resp := e.request(t, http.MethodGet, "/", "", "", "", "")
	for k, want := range map[string]string{
		"X-Frame-Options":         "DENY",
		"Content-Security-Policy": "frame-ancestors 'none'",
		"X-Content-Type-Options":  "nosniff",
	} {
		if got := resp.Header.Get(k); got != want {
			t.Errorf("%s = %q，want %q", k, got, want)
		}
	}
	body(t, resp)

	// 拒绝的响应也带头。
	resp = e.request(t, http.MethodGet, "/api/tiers", "", "", "", "")
	if resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Errorf("401 响应没有 X-Frame-Options：%q", resp.Header.Get("X-Frame-Options"))
	}
	body(t, resp)
}

// AC-3：Host 头不是 127.0.0.1:<端口> 或 localhost:<端口> 时，返回 403。
func TestHostGuard(t *testing.T) {
	e := newEnv(t)
	bad := map[string]string{
		"其他域名":     "evil.com",
		"其他域名带端口":  "evil.com:8080",
		"回环网段外的地址": "10.0.0.5:8080",
		"IPv6 回环":  "[::1]:8080",
		"没有端口的回环":  "127.0.0.1",
		"没有端口的本机名": "localhost",
		"空端口":      "127.0.0.1:",
	}
	for name, host := range bad {
		resp := e.request(t, http.MethodGet, "/api/tiers", "", fakeToken, host, "")
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s（%s）：状态码 = %d，want %d", name, host, resp.StatusCode, http.StatusForbidden)
		}
		body(t, resp)
	}
	if got := e.svc.recorded(); len(got) != 0 {
		t.Errorf("依赖被调用了 %d 次，want 0：%q", len(got), got)
	}
}

// Host 头是 127.0.0.1:<端口> 或 localhost:<端口> 时通过检查，主机名不区分大小写。
func TestHostGuardAllowsLocal(t *testing.T) {
	e := newEnv(t)
	good := map[string]string{
		"回环地址":  "127.0.0.1:1",
		"本机名大写": "LOCALHOST:2",
		"本机名小写": "localhost:3",
	}
	for name, host := range good {
		resp := e.request(t, http.MethodGet, "/api/tiers", "", fakeToken, host, "")
		if resp.StatusCode == http.StatusForbidden {
			t.Errorf("%s（%s）应该通过 Host 检查", name, host)
		}
		body(t, resp)
	}
}

// AC-4：带 Origin 头的跨源请求被拒绝，同源请求放行。
// 浏览器对同源的 POST 和 PUT 也带 Origin 头，所以同源值必须放行。
func TestOriginGuard(t *testing.T) {
	e := newEnv(t)
	origin := strings.TrimPrefix(e.srv.URL, "http://")
	bad := map[string]string{
		"其他网页":     "http://evil.com",
		"空 Origin": "null",
		"https 源":  "https://" + origin,
		"其他本机端口":   "http://127.0.0.1:1",
	}
	for name, o := range bad {
		resp := e.request(t, http.MethodPost, "/api/cabins", `{}`, fakeToken, "", o)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s（%s）：状态码 = %d，want %d", name, o, resp.StatusCode, http.StatusForbidden)
		}
		body(t, resp)
	}
	if got := e.svc.recorded(); len(got) != 0 {
		t.Errorf("依赖被调用了 %d 次，want 0：%q", len(got), got)
	}

	// 同源请求放行。
	resp := e.request(t, http.MethodPost, "/api/cabins", `{}`, fakeToken, "", "http://"+origin)
	if resp.StatusCode == http.StatusForbidden {
		t.Errorf("同源请求被拒绝")
	}
	body(t, resp)
}
