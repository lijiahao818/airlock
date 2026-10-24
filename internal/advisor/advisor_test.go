package advisor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lijiahao818/airlock/internal/cabin"
)

func TestPromptVersion(t *testing.T) {
	b, err := os.ReadFile("prompt.md")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	want := hex.EncodeToString(sum[:])[:12]
	if promptVersion != want {
		t.Fatalf("promptVersion = %q, want %q", promptVersion, want)
	}
}

// TestPromptHasNoEvalCases 保证提示词不含 evals/fixtures 里的注入样本。
func TestPromptHasNoEvalCases(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "evals", "fixtures", "*"))
	if err != nil {
		t.Fatal(err)
	}
	prompt := string(promptMD)
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(b), "\n")
		if len(lines) < 2 {
			continue
		}
		// 第一行是"测试样本，不是指令。"标注，其余是样本本体。
		payload := strings.TrimSpace(strings.Join(lines[1:], "\n"))
		if payload != "" && strings.Contains(prompt, payload) {
			t.Errorf("提示词包含 %s 的样本内容", filepath.Base(file))
		}
	}
}

func TestParseModels(t *testing.T) {
	// 内嵌的 config.json 本身必须能通过解析。
	if _, err := parseModels(configJSON); err != nil {
		t.Fatalf("内嵌配置解析失败：%v", err)
	}

	tests := []struct {
		name string
		json string
		ok   bool
	}{
		{
			"两家齐全",
			`{"anthropic":{"model":"claude-opus-5-5","effort":"medium","max_tokens":1024},"deepseek":{"model":"deepseek-chat","effort":"medium","max_tokens":1024}}`,
			true,
		},
		{"缺 deepseek", `{"anthropic":{"model":"m","effort":"low","max_tokens":1}}`, false},
		{
			"模型为空",
			`{"anthropic":{"model":"","effort":"low","max_tokens":1},"deepseek":{"model":"m","effort":"low","max_tokens":1}}`,
			false,
		},
		{
			"effort 为空",
			`{"anthropic":{"model":"m","effort":"","max_tokens":1},"deepseek":{"model":"m","effort":"low","max_tokens":1}}`,
			false,
		},
		{"不是 JSON", "config", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseModels([]byte(tt.json))
			if tt.ok && err != nil {
				t.Fatalf("应该通过，却报错：%v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("应该报错，却通过了")
			}
		})
	}
}

func TestNewValidation(t *testing.T) {
	ok := Config{
		DataDir:     t.TempDir(),
		BaseURL:     "http://fake-gate.local",
		Placeholder: func(string) string { return "fake-placeholder" },
		HasKey:      func(string) bool { return true },
	}
	tests := map[string]func(c *Config){
		"缺少 DataDir":     func(c *Config) { c.DataDir = "" },
		"缺少 BaseURL":     func(c *Config) { c.BaseURL = "" },
		"缺少 Placeholder": func(c *Config) { c.Placeholder = nil },
		"缺少 HasKey":      func(c *Config) { c.HasKey = nil },
	}
	for name, breakCfg := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := ok
			breakCfg(&cfg)
			if a, err := New(cfg); err == nil {
				t.Fatal("应该报错，却通过了")
			} else if a != nil {
				t.Fatal("报错时不应返回 Advisor")
			}
		})
	}
}

func TestSettingsPersist(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		DataDir:     dir,
		BaseURL:     "http://fake-gate.local",
		Placeholder: func(string) string { return "fake-placeholder" },
		HasKey:      func(string) bool { return true },
	}
	first, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := first.Settings(); got.Enabled {
		t.Fatalf("初始设置 Enabled = true，想 false")
	}
	want := Settings{Enabled: true, Provider: "deepseek", NoticeShown: true}
	if err := first.SetSettings(want); err != nil {
		t.Fatal(err)
	}

	// 重新 New 后读回同样的设置。
	second, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := second.Settings(); got != want {
		t.Fatalf("Settings = %+v, want %+v", got, want)
	}
}

func TestSetSettingsRejectsUnknownProvider(t *testing.T) {
	a, err := New(Config{
		DataDir:     t.TempDir(),
		BaseURL:     "http://fake-gate.local",
		Placeholder: func(string) string { return "fake-placeholder" },
		HasKey:      func(string) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetSettings(Settings{Enabled: true, Provider: "fake-provider"}); err == nil {
		t.Fatal("应该报错，却通过了")
	}
}

// fakeAdvice 是 mock 模型默认返回的一条合法解释。
const fakeAdvice = `{"what":"agent 请求连接 fake-site.example。","risk":"该主机不在允许列表。","risk_level":"medium","action":"检查代码后决定。","suggest_allow":false}`

// fakeModel 模拟副驾专用闸门：记录每个请求，返回固定的模型响应。
// 行为字段必须在第一次 Explain 前设置，之后不能改。
type fakeModel struct {
	srv *httptest.Server

	mu     sync.Mutex
	paths  []string
	keys   []string
	bodies []string

	status int    // 非 0 时返回这个状态码
	text   string // 模型输出的文本
	stop   string // stop_reason
	delay  time.Duration
	block  chan struct{} // 非 nil 时阻塞请求，直到通道关闭
}

func newFakeModel(t *testing.T) *fakeModel {
	f := &fakeModel{text: fakeAdvice, stop: "end_turn"}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		f.keys = append(f.keys, r.Header.Get("X-Api-Key"))
		f.bodies = append(f.bodies, string(b))
		f.mu.Unlock()
		if f.delay > 0 {
			time.Sleep(f.delay)
		}
		if f.block != nil {
			select {
			case <-f.block:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if f.status != 0 {
			w.WriteHeader(f.status)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"api_error","message":"fake failure"}}`))
			return
		}
		msg := map[string]any{
			"id": "msg_fake", "type": "message", "role": "assistant", "model": "fake-model",
			"content":     []map[string]any{{"type": "text", "text": f.text}},
			"stop_reason": f.stop,
			"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
		}
		_ = json.NewEncoder(w).Encode(msg)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeModel) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.paths)
}

func (f *fakeModel) lastBody(t *testing.T) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) == 0 {
		t.Fatal("fakeModel 没有收到请求")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(f.bodies[len(f.bodies)-1]), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func (f *fakeModel) lastRequest(t *testing.T) (path, key string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.paths) == 0 {
		t.Fatal("fakeModel 没有收到请求")
	}
	return f.paths[len(f.paths)-1], f.keys[len(f.keys)-1]
}

// newTestAdvisor 用本地 mock 闸门建一个开启状态的 Advisor。keys 列出有密钥的供应商。
func newTestAdvisor(t *testing.T, f *fakeModel, keys ...string) *Advisor {
	t.Helper()
	has := map[string]bool{}
	for _, k := range keys {
		has[k] = true
	}
	a, err := New(Config{
		DataDir: t.TempDir(),
		BaseURL: f.srv.URL,
		Placeholder: func(p string) string {
			if has[p] {
				return "fake-placeholder-" + p
			}
			return ""
		},
		HasKey: func(p string) bool { return has[p] },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetSettings(Settings{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	return a
}

func fakeMeta(host string) cabin.HoldMeta {
	return cabin.HoldMeta{
		Agent: "claude-code", Host: host, Port: 443,
		Method: "GET", Path: "/v1/data", Rule: "hold",
	}
}

func TestExplainOff(t *testing.T) {
	f := newFakeModel(t)
	t.Run("副驾没有开启", func(t *testing.T) {
		a := newTestAdvisor(t, f, "anthropic")
		if err := a.SetSettings(Settings{}); err != nil {
			t.Fatal(err)
		}
		if _, err := a.Explain(context.Background(), "run-1", fakeMeta("fake-site.example")); !errors.Is(err, cabin.ErrAdvisorOff) {
			t.Fatalf("err = %v, want ErrAdvisorOff", err)
		}
	})
	t.Run("两家都没有密钥", func(t *testing.T) {
		a := newTestAdvisor(t, f)
		if _, err := a.Explain(context.Background(), "run-1", fakeMeta("fake-site.example")); !errors.Is(err, cabin.ErrAdvisorOff) {
			t.Fatalf("err = %v, want ErrAdvisorOff", err)
		}
	})
	if n := f.count(); n != 0 {
		t.Fatalf("副驾关闭时不应调用模型，调用了 %d 次", n)
	}
}

func TestProviderSelection(t *testing.T) {
	t.Run("只有 anthropic 的密钥", func(t *testing.T) {
		f := newFakeModel(t)
		a := newTestAdvisor(t, f, "anthropic")
		if _, err := a.Explain(context.Background(), "run-1", fakeMeta("fake-site.example")); err != nil {
			t.Fatal(err)
		}
		if path, key := f.lastRequest(t); path != "/anthropic/v1/messages" || key != "fake-placeholder-anthropic" {
			t.Fatalf("请求 = (%q, %q)", path, key)
		}
	})
	t.Run("只有 deepseek 的密钥", func(t *testing.T) {
		f := newFakeModel(t)
		a := newTestAdvisor(t, f, "deepseek")
		if _, err := a.Explain(context.Background(), "run-1", fakeMeta("fake-site.example")); err != nil {
			t.Fatal(err)
		}
		if path, key := f.lastRequest(t); path != "/deepseek/v1/messages" || key != "fake-placeholder-deepseek" {
			t.Fatalf("请求 = (%q, %q)", path, key)
		}
	})
	t.Run("两家都有时用设置的供应商", func(t *testing.T) {
		f := newFakeModel(t)
		a := newTestAdvisor(t, f, "anthropic", "deepseek")
		if err := a.SetSettings(Settings{Enabled: true, Provider: "deepseek"}); err != nil {
			t.Fatal(err)
		}
		if _, err := a.Explain(context.Background(), "run-1", fakeMeta("fake-site.example")); err != nil {
			t.Fatal(err)
		}
		if path, key := f.lastRequest(t); path != "/deepseek/v1/messages" || key != "fake-placeholder-deepseek" {
			t.Fatalf("请求 = (%q, %q)", path, key)
		}
	})
	t.Run("两家都有但没有选择供应商", func(t *testing.T) {
		f := newFakeModel(t)
		a := newTestAdvisor(t, f, "anthropic", "deepseek")
		if _, err := a.Explain(context.Background(), "run-1", fakeMeta("fake-site.example")); !errors.Is(err, errNoProvider) {
			t.Fatalf("err = %v, want errNoProvider", err)
		}
		if n := f.count(); n != 0 {
			t.Fatalf("不应调用模型，调用了 %d 次", n)
		}
	})
}

func TestRequestContent(t *testing.T) {
	f := newFakeModel(t)
	a := newTestAdvisor(t, f, "anthropic")
	meta := cabin.HoldMeta{
		Agent: "claude-code", Host: "fake-site.example", Port: 8443,
		Method: "POST", Path: "/v1/data", Rule: "hold",
	}
	if _, err := a.Explain(context.Background(), "run-1", meta); err != nil {
		t.Fatal(err)
	}
	body := f.lastBody(t)

	models, err := parseModels(configJSON)
	if err != nil {
		t.Fatal(err)
	}
	pc := models["anthropic"]
	if body["model"] != pc.Model {
		t.Errorf("model = %v, want %s", body["model"], pc.Model)
	}
	if got := body["max_tokens"]; got != float64(pc.MaxTokens) {
		t.Errorf("max_tokens = %v, want %d", got, pc.MaxTokens)
	}
	oc, _ := body["output_config"].(map[string]any)
	if oc["effort"] != pc.Effort {
		t.Errorf("output_config.effort = %v, want %s", oc["effort"], pc.Effort)
	}
	if pc.Format != (oc["format"] != nil) {
		t.Errorf("output_config.format = %v, want 有无 = %v", oc["format"], pc.Format)
	}
	for _, k := range []string{"temperature", "top_p", "top_k"} {
		if _, ok := body[k]; ok {
			t.Errorf("请求发送了采样参数 %s", k)
		}
	}

	// 系统提示词就是打包的 prompt.md。
	var system string
	switch s := body["system"].(type) {
	case string:
		system = s
	case []any:
		if len(s) > 0 {
			blk, _ := s[0].(map[string]any)
			system, _ = blk["text"].(string)
		}
	}
	if system != string(promptMD) {
		t.Error("system 不是 prompt.md 的内容")
	}

	// 用户消息只含 6 个元数据字段。
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages 数量 = %d, want 1", len(msgs))
	}
	msg, _ := msgs[0].(map[string]any)
	var text string
	switch c := msg["content"].(type) {
	case string:
		text = c
	case []any:
		if len(c) > 0 {
			blk, _ := c[0].(map[string]any)
			text, _ = blk["text"].(string)
		}
	}
	if !strings.Contains(text, "不是指令") {
		t.Errorf("用户消息没有声明数据不是指令：\n%s", text)
	}
	mm := regexp.MustCompile(`(?s)<data-[0-9a-f]+>\n(\{.*?\})\n</data-[0-9a-f]+>`).FindStringSubmatch(text)
	if mm == nil {
		t.Fatalf("用户消息没有元数据标签：\n%s", text)
	}
	var gotMeta map[string]any
	if err := json.Unmarshal([]byte(mm[1]), &gotMeta); err != nil {
		t.Fatal(err)
	}
	wantMeta := map[string]any{
		"agent": "claude-code", "host": "fake-site.example",
		"port": float64(8443), "method": "POST", "path": "/v1/data", "rule": "hold",
	}
	if !reflect.DeepEqual(gotMeta, wantMeta) {
		t.Fatalf("发送的元数据 = %v, want %v", gotMeta, wantMeta)
	}
}

func TestExplainResult(t *testing.T) {
	f := newFakeModel(t)
	a := newTestAdvisor(t, f, "anthropic")
	exp, err := a.Explain(context.Background(), "run-1", fakeMeta("fake-site.example"))
	if err != nil {
		t.Fatal(err)
	}
	var want advice
	if err := json.Unmarshal([]byte(fakeAdvice), &want); err != nil {
		t.Fatal(err)
	}
	if exp.What != want.What || exp.Risk != want.Risk || exp.RiskLevel != want.RiskLevel ||
		exp.Action != want.Action || exp.SuggestAllow != want.SuggestAllow {
		t.Fatalf("Explanation = %+v", exp)
	}
	if exp.Source != "ai" {
		t.Errorf("Source = %q, want ai", exp.Source)
	}
	if exp.PromptVersion != promptVersion {
		t.Errorf("PromptVersion = %q, want %q", exp.PromptVersion, promptVersion)
	}
	if exp.AIUnavailable {
		t.Error("AIUnavailable = true")
	}
}

func TestOutputInvalid(t *testing.T) {
	long := strings.Repeat("风", maxFieldRunes+1)
	tests := []struct {
		name string
		text string
		stop string
	}{
		{"不是 JSON", "解释", "end_turn"},
		{"未知风险等级", `{"what":"a","risk":"b","risk_level":"extreme","action":"c","suggest_allow":true}`, "end_turn"},
		{"字段超长", `{"what":"` + long + `","risk":"b","risk_level":"low","action":"c","suggest_allow":true}`, "end_turn"},
		{"缺少字段", `{"risk":"b","risk_level":"low","action":"c","suggest_allow":true}`, "end_turn"},
		{"有多余字段", `{"what":"a","risk":"b","risk_level":"low","action":"c","suggest_allow":true,"extra":"x"}`, "end_turn"},
		{"文字字段为空", `{"what":"","risk":"b","risk_level":"low","action":"c","suggest_allow":true}`, "end_turn"},
		{"字段类型错误", `{"what":"a","risk":"b","risk_level":"low","action":"c","suggest_allow":"yes"}`, "end_turn"},
		{"被 max_tokens 截断", fakeAdvice, "max_tokens"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeModel(t)
			f.text, f.stop = tt.text, tt.stop
			a := newTestAdvisor(t, f, "anthropic")
			if _, err := a.Explain(context.Background(), "run-1", fakeMeta("fake-site.example")); err == nil {
				t.Fatal("应该报错，却通过了")
			}
		})
	}
}

func TestCallFailure(t *testing.T) {
	f := newFakeModel(t)
	f.status = http.StatusInternalServerError
	a := newTestAdvisor(t, f, "anthropic")
	_, err := a.Explain(context.Background(), "run-1", fakeMeta("fake-site.example"))
	if err == nil || !strings.Contains(err.Error(), "模型调用失败") {
		t.Fatalf("err = %v, want 模型调用失败", err)
	}
}

func TestNoPlaceholder(t *testing.T) {
	f := newFakeModel(t)
	a, err := New(Config{
		DataDir:     t.TempDir(),
		BaseURL:     f.srv.URL,
		Placeholder: func(string) string { return "" },
		HasKey:      func(string) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetSettings(Settings{Enabled: true, Provider: "anthropic"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Explain(context.Background(), "run-1", fakeMeta("fake-site.example")); !errors.Is(err, errNoPlaceholder) {
		t.Fatalf("err = %v, want errNoPlaceholder", err)
	}
	if n := f.count(); n != 0 {
		t.Fatalf("不应发起请求，发了 %d 次", n)
	}
}

func TestTimeout(t *testing.T) {
	saved := callTimeout
	callTimeout = 50 * time.Millisecond
	t.Cleanup(func() { callTimeout = saved })

	f := newFakeModel(t)
	f.delay = 500 * time.Millisecond
	a := newTestAdvisor(t, f, "anthropic")
	if _, err := a.Explain(context.Background(), "run-1", fakeMeta("fake-site.example")); err == nil {
		t.Fatal("应该报错，却通过了")
	}
}

func TestCtxCancel(t *testing.T) {
	f := newFakeModel(t)
	f.block = make(chan struct{})
	t.Cleanup(func() { close(f.block) })
	a := newTestAdvisor(t, f, "anthropic")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	if _, err := a.Explain(ctx, "run-1", fakeMeta("fake-site.example")); err == nil {
		t.Fatal("应该报错，却通过了")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("取消后没有立即返回，耗时 %s", elapsed)
	}
}

func TestHostCache(t *testing.T) {
	f := newFakeModel(t)
	a := newTestAdvisor(t, f, "anthropic")
	ctx := context.Background()
	first, err := a.Explain(ctx, "run-1", fakeMeta("fake-site.example"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.Explain(ctx, "run-1", fakeMeta("fake-site.example"))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("两次解释不同：%+v vs %+v", first, second)
	}
	if n := f.count(); n != 1 {
		t.Fatalf("同一主机调用了 %d 次, want 1", n)
	}
	// 不同主机不共享缓存。
	if _, err := a.Explain(ctx, "run-1", fakeMeta("fake-other.example")); err != nil {
		t.Fatal(err)
	}
	if n := f.count(); n != 2 {
		t.Fatalf("不同主机调用了 %d 次, want 2", n)
	}
}

func TestHostCacheConcurrent(t *testing.T) {
	f := newFakeModel(t)
	a := newTestAdvisor(t, f, "anthropic")
	var wg sync.WaitGroup
	exps := make([]cabin.Explanation, 5)
	for i := range exps {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			exp, err := a.Explain(context.Background(), "run-1", fakeMeta("fake-site.example"))
			if err != nil {
				t.Error(err)
				return
			}
			exps[i] = exp
		}(i)
	}
	wg.Wait()
	if n := f.count(); n != 1 {
		t.Fatalf("同一主机调用了 %d 次, want 1", n)
	}
	for i, exp := range exps[1:] {
		if exp != exps[0] {
			t.Fatalf("第 %d 个解释与第一个不同", i+1)
		}
	}
}

func TestCallLimit(t *testing.T) {
	f := newFakeModel(t)
	a := newTestAdvisor(t, f, "anthropic")
	ctx := context.Background()
	for i := 0; i < maxCallsPerRun; i++ {
		host := fmt.Sprintf("fake-h%d.example", i)
		if _, err := a.Explain(ctx, "run-1", fakeMeta(host)); err != nil {
			t.Fatalf("第 %d 次调用失败：%v", i+1, err)
		}
	}
	if _, err := a.Explain(ctx, "run-1", fakeMeta("fake-over.example")); !errors.Is(err, errCallLimit) {
		t.Fatalf("err = %v, want errCallLimit", err)
	}
	if n := f.count(); n != maxCallsPerRun {
		t.Fatalf("模型调用 %d 次, want %d", n, maxCallsPerRun)
	}
	// 已缓存的主机不再调用，也不占次数。
	if _, err := a.Explain(ctx, "run-1", fakeMeta("fake-h0.example")); err != nil {
		t.Fatal(err)
	}
	// 新的运行有独立的次数。
	if _, err := a.Explain(ctx, "run-2", fakeMeta("fake-new.example")); err != nil {
		t.Fatal(err)
	}
	if n := f.count(); n != maxCallsPerRun+1 {
		t.Fatalf("模型调用 %d 次, want %d", n, maxCallsPerRun+1)
	}
}

func TestFailedCallNotCached(t *testing.T) {
	f := newFakeModel(t)
	a := newTestAdvisor(t, f, "anthropic")
	ctx := context.Background()
	f.status = http.StatusInternalServerError
	if _, err := a.Explain(ctx, "run-1", fakeMeta("fake-site.example")); err == nil {
		t.Fatal("应该报错，却通过了")
	}
	// 失败不缓存，同一个主机可以重试。
	f.status = 0
	if _, err := a.Explain(ctx, "run-1", fakeMeta("fake-site.example")); err != nil {
		t.Fatalf("失败后不能重试：%v", err)
	}
	if n := f.count(); n != 2 {
		t.Fatalf("模型调用 %d 次, want 2", n)
	}
}
