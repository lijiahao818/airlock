package gate

import (
	"context"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestContractValues(t *testing.T) {
	if got, want := Providers(), []string{"anthropic", "openai", "deepseek"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Providers() = %v, want %v", got, want)
	}
	if got, want := Tiers(), []string{"strict", "standard", "permissive"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Tiers() = %v, want %v", got, want)
	}
}

func TestRejectUnknownTier(t *testing.T) {
	if hosts, err := TierHosts("fake-tier"); err == nil || hosts != nil {
		t.Fatalf("TierHosts(fake-tier) = %v, %v; want nil, error", hosts, err)
	}
}

// 未知供应商必须在发出网络请求前被拒绝。
func TestRejectUnknownProviderKey(t *testing.T) {
	if err := TestKey(context.Background(), "fake-provider", "fake-key"); err == nil {
		t.Fatal("TestKey 接受了未知的供应商")
	}
}

// recorder 收集 OnEvent 收到的事件。
type recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *recorder) add(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) rules() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var rules []string
	for _, e := range r.events {
		rules = append(rules, e.Rule)
	}
	return rules
}

func (r *recorder) last(t *testing.T) Event {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) == 0 {
		t.Fatal("OnEvent 没有被调用")
	}
	return r.events[len(r.events)-1]
}

// startGate 用测试默认值补全 cfg 并启动闸门。
func startGate(t *testing.T, cfg Config) (*Gate, *recorder) {
	t.Helper()
	rec := &recorder{}
	if cfg.Secret == nil {
		cfg.Secret = func(string) (string, error) { return fakeSecret, nil }
	}
	if cfg.Ask == nil {
		cfg.Ask = func(context.Context, Event) bool { return false }
	}
	cfg.OnEvent = rec.add
	g, err := Start(cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g, rec
}

func (g *Gate) url() string { return "http://127.0.0.1:" + strconv.Itoa(g.Port()) }

func TestStartRejectsBadConfig(t *testing.T) {
	ok := func(string) (string, error) { return "", nil }
	ask := func(context.Context, Event) bool { return false }
	for name, cfg := range map[string]Config{
		"缺少 Secret": {Ask: ask},
		"缺少 Ask":    {Secret: ok},
		"未知供应商":     {Secret: ok, Ask: ask, Providers: []string{"fake-provider"}},
	} {
		if g, err := Start(cfg); err == nil {
			g.Close()
			t.Errorf("%s：Start 没有报错", name)
		}
	}
}

func TestPlaceholders(t *testing.T) {
	a, _ := startGate(t, Config{})
	b, _ := startGate(t, Config{})
	for _, name := range Providers() {
		p := a.Placeholder(name)
		if p != a.Placeholder(name) {
			t.Errorf("%s 的占位符每次不同", name)
		}
		if p == b.Placeholder(name) {
			t.Errorf("两个实例的 %s 占位符相同", name)
		}
		// 16 字节随机数编码为 32 个十六进制字符。
		if !strings.HasPrefix(p, "airlock-") || len(p) != len("airlock-")+32 {
			t.Errorf("%s 的占位符格式不对：%q", name, p)
		}
	}
	if a.Placeholder("anthropic") == a.Placeholder("openai") {
		t.Error("不同供应商的占位符相同")
	}
	if a.Placeholder("fake-provider") != "" {
		t.Error("未知供应商有占位符")
	}
}

func get(t *testing.T, g *Gate, host, path string, header http.Header) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, g.url()+path, nil)
	if host != "" {
		req.Host = host
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求闸门：%v", err)
	}
	resp.Body.Close()
	return resp
}

func TestRejectBadHost(t *testing.T) {
	g, rec := startGate(t, Config{})
	// Host 为空时只接受 127.0.0.1:<闸门端口>。
	for _, host := range []string{"fake-evil.example", "localhost:" + strconv.Itoa(g.Port()), "127.0.0.1"} {
		if resp := get(t, g, host, "/anthropic/v1/messages", nil); resp.StatusCode != http.StatusForbidden {
			t.Errorf("Host %q：状态码 %d，want 403", host, resp.StatusCode)
		}
		if e := rec.last(t); e.Rule != "bad-host" || e.Allowed {
			t.Errorf("Host %q：事件 %+v，want bad-host", host, e)
		}
	}
}

func TestConfigHost(t *testing.T) {
	g, rec := startGate(t, Config{Host: "airlock-gate:8080"})
	get(t, g, "", "/", nil)
	if e := rec.last(t); e.Rule != "bad-host" {
		t.Errorf("Config.Host 不为空时接受了 127.0.0.1：%+v", e)
	}
	get(t, g, "airlock-gate:8080", "/", nil)
	if e := rec.last(t); e.Rule != "unsupported" {
		t.Errorf("Config.Host 的请求被判为 %s", e.Rule)
	}
}

func TestRejectUnsupported(t *testing.T) {
	g, rec := startGate(t, Config{})
	resp := get(t, g, "", "/fake-path?q=fake-query", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("状态码 %d，want 404", resp.StatusCode)
	}
	if e := rec.last(t); e.Rule != "unsupported" || e.Allowed || e.Path != "" {
		t.Errorf("事件 %+v，want 没有路径的 unsupported", e)
	}
}
