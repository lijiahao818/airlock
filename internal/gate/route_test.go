package gate

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// upstreamHits 记录 mock 上游收到的最后一个请求。
type upstreamHits struct {
	n      atomic.Int32
	last   atomic.Pointer[http.Request]
	status int
}

func (u *upstreamHits) handler(w http.ResponseWriter, r *http.Request) {
	u.n.Add(1)
	u.last.Store(r.Clone(r.Context()))
	if u.status != 0 {
		w.WriteHeader(u.status)
	}
	io.WriteString(w, "fake-upstream-ok")
}

// post 用 POST 请求闸门。模型路由只转发 POST。
func post(t *testing.T, g *Gate, path string, header http.Header) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, g.url()+path, strings.NewReader("fake-body"))
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求闸门：%v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func keyHeader(g *Gate, name string) http.Header {
	p := providers[name]
	v := g.Placeholder(name)
	if p.bearer {
		v = "Bearer " + v
	}
	return http.Header{http.CanonicalHeaderKey(p.header): {v}}
}

func TestRouteReplacesPlaceholder(t *testing.T) {
	up := &upstreamHits{}
	mockUpstream(t, up.handler)
	g, rec := startGate(t, Config{Providers: Providers()})
	for _, name := range Providers() {
		h := keyHeader(g, name)
		// 沙箱带来的其他凭据必须被去掉。
		h.Set("Proxy-Authorization", "fake-sandbox-cred")
		if name != "openai" {
			h.Set("Authorization", "Bearer fake-sandbox-cred")
		}
		path := "/v1/messages"
		if name == "openai" {
			path = "/v1/responses"
		}
		resp := post(t, g, "/"+name+path+"?beta=true", h)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s：状态码 %d", name, resp.StatusCode)
		}
		r := up.last.Load()
		p := providers[name]
		want := fakeSecret
		if p.bearer {
			want = "Bearer " + fakeSecret
		}
		if got := r.Header.Get(p.header); got != want {
			t.Errorf("%s：上游收到 %s = %q, want %q", name, p.header, got, want)
		}
		for k, vs := range r.Header {
			for _, v := range vs {
				if strings.Contains(v, g.Placeholder(name)) || strings.Contains(v, "fake-sandbox-cred") {
					t.Errorf("%s：上游收到了 %s: %s", name, k, v)
				}
			}
		}
		if r.URL.Path != "/"+name+"-upstream"+path || r.URL.RawQuery != "beta=true" {
			t.Errorf("%s：上游收到 %s?%s", name, r.URL.Path, r.URL.RawQuery)
		}
		e := rec.last(t)
		if e.Rule != "route" || !e.Allowed || e.Provider != name || e.Path != "/"+name+path || e.Port == 0 {
			t.Errorf("%s：事件 %+v", name, e)
		}
	}
}

func TestDeepseekUpstreamUsesAnthropicFormat(t *testing.T) {
	if got := providers["deepseek"].upstream; got != "https://api.deepseek.com/anthropic" {
		t.Fatalf("deepseek 的上游地址 = %s", got)
	}
}

func TestRouteRejectsBadPlaceholder(t *testing.T) {
	up := &upstreamHits{}
	mockUpstream(t, up.handler)
	g, rec := startGate(t, Config{Providers: Providers()})
	other, _ := startGate(t, Config{Providers: Providers()})
	cases := map[string]http.Header{
		"没有占位符":      nil,
		"错误的占位符":     {"X-Api-Key": {"fake-wrong"}},
		"另一个实例的占位符":  keyHeader(other, "anthropic"),
		"另一个供应商的占位符": {"X-Api-Key": {g.Placeholder("openai")}},
		"占位符放错请求头":   {"Authorization": {"Bearer " + g.Placeholder("anthropic")}},
	}
	for name, h := range cases {
		resp := get(t, g, "", "/anthropic/v1/messages", h)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s：状态码 %d，want 403", name, resp.StatusCode)
		}
		if e := rec.last(t); e.Rule != "bad-placeholder" || e.Allowed || e.Provider != "" {
			t.Errorf("%s：事件 %+v", name, e)
		}
	}
	if n := up.n.Load(); n != 0 {
		t.Fatalf("上游收到了 %d 个请求", n)
	}
}

func TestRouteRejectsClosedRoute(t *testing.T) {
	up := &upstreamHits{}
	mockUpstream(t, up.handler)
	g, rec := startGate(t, Config{Providers: []string{"anthropic"}})
	get(t, g, "", "/openai/v1/responses", keyHeader(g, "openai"))
	if e := rec.last(t); e.Rule != "unsupported" || e.Allowed {
		t.Errorf("未开放的路由：事件 %+v", e)
	}
	if up.n.Load() != 0 {
		t.Fatal("未开放的路由把请求发给了上游")
	}
}

func TestRouteRejectsUncleanPath(t *testing.T) {
	up := &upstreamHits{}
	mockUpstream(t, up.handler)
	g, rec := startGate(t, Config{Providers: Providers()})
	for _, p := range []string{"/deepseek/../openai/v1", "/anthropic//v1/messages", "/anthropic/v1/", "/anthropic/./v1"} {
		get(t, g, "", p, keyHeader(g, "deepseek"))
		if e := rec.last(t); e.Rule != "unsupported" {
			t.Errorf("%s：事件 %+v，want unsupported", p, e)
		}
	}
	if up.n.Load() != 0 {
		t.Fatal("不规范的路径被转发给了上游")
	}
}

func TestRouteCallsSecretEveryTime(t *testing.T) {
	up := &upstreamHits{}
	mockUpstream(t, up.handler)
	var calls atomic.Int32
	g, _ := startGate(t, Config{Providers: Providers(), Secret: func(string) (string, error) {
		calls.Add(1)
		return fakeSecret, nil
	}})
	for range 2 {
		post(t, g, "/anthropic/v1/messages", keyHeader(g, "anthropic"))
	}
	if calls.Load() != 2 {
		t.Fatalf("Secret 被调用 %d 次，want 2", calls.Load())
	}
}

func TestRouteWithoutSecret(t *testing.T) {
	up := &upstreamHits{}
	mockUpstream(t, up.handler)
	g, rec := startGate(t, Config{Providers: Providers(), Secret: func(string) (string, error) {
		return "", errors.New("fake-no-key")
	}})
	resp := post(t, g, "/anthropic/v1/messages", keyHeader(g, "anthropic"))
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("状态码 %d，want 502", resp.StatusCode)
	}
	if e := rec.last(t); e.Rule != "no-key" || e.Allowed || e.Provider != "" {
		t.Errorf("没有密钥时事件 %+v", e)
	}
	if up.n.Load() != 0 {
		t.Fatal("没有密钥时请求被发给了上游")
	}
}

// 上游出错时，返回给沙箱的内容中不能有真实密钥。
func TestRouteUpstreamDown(t *testing.T) {
	srv := mockUpstream(t, func(http.ResponseWriter, *http.Request) {})
	srv.Close()
	g, _ := startGate(t, Config{Providers: Providers()})
	req, _ := http.NewRequest(http.MethodPost, g.url()+"/openai/v1/responses", strings.NewReader("fake-body"))
	req.Header = keyHeader(g, "openai")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || strings.Contains(string(body), fakeSecret) {
		t.Fatalf("状态码 %d，响应 %q", resp.StatusCode, body)
	}
}

// 密钥还能访问账号下的文件和批处理数据，模型路由只转发推理接口。
func TestRouteRejectsOtherPaths(t *testing.T) {
	up := &upstreamHits{}
	mockUpstream(t, up.handler)
	g, rec := startGate(t, Config{Providers: Providers()})
	for _, p := range []string{
		"/anthropic/v1/files", "/anthropic/v1/messages/batches", "/anthropic/v1/models",
		"/openai/v1/files", "/openai/v1/chat/completions", "/deepseek/v1/messages/count_tokens",
	} {
		name := strings.Split(p, "/")[1]
		if resp := post(t, g, p+"?limit=1", keyHeader(g, name)); resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s：状态码 %d，want 403", p, resp.StatusCode)
		}
		if e := rec.last(t); e.Rule != "route-path" || e.Allowed || e.Provider != "" || e.Path != p {
			t.Errorf("%s：事件 %+v", p, e)
		}
	}
	// 允许的路径也只接受 POST。
	if resp := get(t, g, "", "/anthropic/v1/messages", keyHeader(g, "anthropic")); resp.StatusCode != http.StatusForbidden {
		t.Errorf("GET：状态码 %d，want 403", resp.StatusCode)
	}
	if e := rec.last(t); e.Rule != "route-path" || e.Method != http.MethodGet {
		t.Errorf("GET：事件 %+v", e)
	}
	if n := up.n.Load(); n != 0 {
		t.Fatalf("上游收到了 %d 个请求", n)
	}
}

func TestRouteAllowsInferencePaths(t *testing.T) {
	up := &upstreamHits{}
	mockUpstream(t, up.handler)
	g, rec := startGate(t, Config{Providers: Providers()})
	for _, p := range []string{
		"/anthropic/v1/messages", "/anthropic/v1/messages/count_tokens", "/openai/v1/responses", "/deepseek/v1/messages",
	} {
		name := strings.Split(p, "/")[1]
		if resp := post(t, g, p, keyHeader(g, name)); resp.StatusCode != http.StatusOK {
			t.Errorf("%s：状态码 %d，want 200", p, resp.StatusCode)
		}
		if e := rec.last(t); e.Rule != "route" || !e.Allowed {
			t.Errorf("%s：事件 %+v", p, e)
		}
	}
	if n := up.n.Load(); n != 4 {
		t.Fatalf("上游收到了 %d 个请求，want 4", n)
	}
}

// fragments 返回 s 中所有长度不少于 minFragment 的子串。
func fragments(s string) []string {
	var out []string
	for i := range s {
		for j := i + minFragment; j <= len(s); j++ {
			out = append(out, s[i:j])
		}
	}
	return out
}

// OpenAI 的 401 响应写出密钥的前 8 位和后 4 位。沙箱收到的响应中不能有这些片段。
func TestRouteRedactsErrorBody(t *testing.T) {
	msg := `{"error":{"message":"Incorrect API key provided: ` + fakeSecret[:8] + `****` + fakeSecret[len(fakeSecret)-4:] +
		`. Full key: ` + fakeSecret + `","type":"invalid_request_error"}}`
	mockUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 上游按请求协商压缩。闸门必须拿到明文才能打码。
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			w.WriteHeader(http.StatusUnauthorized)
			zw := gzip.NewWriter(w)
			io.WriteString(zw, msg)
			zw.Close()
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, msg)
	})
	g, _ := startGate(t, Config{Providers: Providers()})
	for _, gz := range []bool{false, true} {
		h := keyHeader(g, "openai")
		if gz {
			// 沙箱自己要求压缩时，闸门也不能把压缩的错误响应原样转发。
			h.Set("Accept-Encoding", "gzip")
		}
		resp := post(t, g, "/openai/v1/responses", h)
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("Content-Encoding") != "" {
			t.Fatalf("gzip=%v：状态码 %d，Content-Encoding %q", gz, resp.StatusCode, resp.Header.Get("Content-Encoding"))
		}
		for _, f := range fragments(fakeSecret) {
			if bytes.Contains(body, []byte(f)) {
				t.Fatalf("gzip=%v：响应中有密钥片段 %q：%s", gz, f, body)
			}
		}
		if !bytes.Contains(body, []byte("Incorrect API key provided")) || !bytes.Contains(body, []byte(`"type":"invalid_request_error"`)) {
			t.Errorf("gzip=%v：打码去掉了错误信息：%s", gz, body)
		}
	}
}

// 不能按明文检查的错误响应，整个换成固定文字。
func TestRouteHidesUncheckableErrorBody(t *testing.T) {
	mockUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "br")
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, fakeSecret)
	})
	g, _ := startGate(t, Config{Providers: Providers()})
	resp := post(t, g, "/anthropic/v1/messages", keyHeader(g, "anthropic"))
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("Content-Encoding") != "" || bytes.Contains(body, []byte("1234")) {
		t.Fatalf("状态码 %d，Content-Encoding %q，响应 %q", resp.StatusCode, resp.Header.Get("Content-Encoding"), body)
	}
}

// 打码只处理错误响应。正常的流式响应原样转发，包括其中碰巧与密钥相同的片段。
func TestRouteKeepsStream(t *testing.T) {
	events := []string{
		"event: message_start\ndata: {\"type\":\"message_start\"}\n\n",
		"event: content_block_delta\ndata: {\"text\":\"1234 fake\"}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}
	mockUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			io.WriteString(w, e)
			w.(http.Flusher).Flush()
		}
	})
	g, _ := startGate(t, Config{Providers: Providers()})
	resp := post(t, g, "/anthropic/v1/messages", keyHeader(g, "anthropic"))
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != strings.Join(events, "") {
		t.Fatalf("状态码 %d，响应 %q", resp.StatusCode, body)
	}
}
