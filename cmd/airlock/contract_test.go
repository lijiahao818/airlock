package main

// 本文件验证 docs/architecture.md 第 5 节的契约能否串起主要用户故事。
// 测试扮演控制器（console 的角色），fakeCore 扮演模型（核心层）。
// fakeCore 只用契约中的类型和回调搭建，不代表真实实现。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lijiahao818/airlock/internal/advisor"
	"github.com/lijiahao818/airlock/internal/audit"
	"github.com/lijiahao818/airlock/internal/cabin"
	"github.com/lijiahao818/airlock/internal/console"
	"github.com/lijiahao818/airlock/internal/gate"
	"github.com/lijiahao818/airlock/internal/sandbox"
	"github.com/lijiahao818/airlock/internal/staging"
	"github.com/lijiahao818/airlock/internal/webterm"
)

// 跨层的契约在编译期核对。
var (
	_ cabin.Service           = (*fakeCore)(nil)
	_ webterm.Attacher        = cabin.Service(nil)
	_ console.AdvisorSettings = (*advisor.Advisor)(nil)
)

func TestContractWalkthrough(t *testing.T) {
	core := newFakeCore(fakeExplainer{t: t}, fakeInspector{})
	events, cancel := core.Subscribe()
	defer cancel()
	ctx := context.Background()

	// F-3：创建并启动工作舱。
	c, err := core.Create(`C:\code\demo`, "claude-code", gate.Tiers()[1])
	if err != nil || c.Status != cabin.Idle {
		t.Fatalf("Create = %+v, %v", c, err)
	}
	if err := core.Start(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if e := next(t, events, "status"); e.Status != cabin.Running {
		t.Fatalf("status = %q, want running", e.Status)
	}

	// F-5：进舱检查排除 .env。
	entry, _ := core.EntryFindings(c.ID)
	if len(entry) != 1 || entry[0].Kind != cabin.Excluded {
		t.Fatalf("EntryFindings = %+v", entry)
	}

	// F-6：网页终端用 AgentURL 中的令牌连接。错误的令牌被拒绝。
	raw, _ := core.AgentURL(c.ID)
	u, err := url.Parse(raw)
	if err != nil || u.Query().Get("cabin") != c.ID {
		t.Fatalf("AgentURL = %q", raw)
	}
	token := strings.TrimPrefix(u.Fragment, "token=")
	if _, err := core.Attach(ctx, c.ID, "fake-wrong-token"); err == nil {
		t.Fatal("Attach 接受了错误的令牌")
	}
	s, err := core.Attach(ctx, c.ID, token)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	s.Write([]byte("hi"))
	out := make([]byte, 2)
	if n, _ := s.Read(out); string(out[:n]) != "hi" {
		t.Fatalf("终端输出 = %q", out[:n])
	}

	// F-7：卡片先显示模板解释，再替换为 AI 解释。用户拒绝。
	h := next(t, events, "hold").Hold
	if h.Explanation.Source != "template" || h.Host != "evil.example" {
		t.Fatalf("hold = %+v", h)
	}
	if ex := next(t, events, "explained").Hold; ex.ID != h.ID || ex.Explanation.Source != "ai" {
		t.Fatalf("explained = %+v", ex)
	}
	wantJSON(t, h, "id", "host", "port", "rule", "explanation")
	if err := core.Decide(h.ID, false); err != nil {
		t.Fatal(err)
	}
	if r := next(t, events, "request").Request; r.Allowed || r.Rule != "user-deny" {
		t.Fatalf("request = %+v", r)
	}

	// F-8：停止运行，出舱审查标出高风险改动，用户只应用普通改动。
	if err := core.Stop(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	items, _ := core.ReviewItems(c.ID)
	if len(items) != 2 || len(items[1].Findings) != 1 || items[1].Findings[0].Kind != cabin.HighRisk {
		t.Fatalf("ReviewItems = %+v", items)
	}
	wantJSON(t, items[1], "path", "kind", "conflict", "special", "findings")
	if err := core.Apply(c.ID, items[0].Path); err != nil {
		t.Fatal(err)
	}
	if err := core.FinishReview(c.ID); err != nil {
		t.Fatal(err)
	}

	// F-9：运行报告记录暂扣决定和文件决定。
	rep, _ := core.Report(c.ID)
	if len(rep.Holds) != 1 || rep.Holds[0].Rule != "user-deny" {
		t.Fatalf("Holds = %+v", rep.Holds)
	}
	if len(rep.Files) != 2 || !rep.Files[0].Applied || rep.Files[1].Applied {
		t.Fatalf("Files = %+v", rep.Files)
	}
	wantJSON(t, rep, "hosts", "holds", "keyUses", "files")
	list, _ := core.List()
	if list[0].Status != cabin.Idle {
		t.Fatalf("status = %q, want idle", list[0].Status)
	}
}

// next 读取事件，直到遇到指定类型。
func next(t *testing.T, ch <-chan cabin.Event, kind string) cabin.Event {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case e := <-ch:
			if e.Kind == kind {
				return e
			}
		case <-timeout:
			t.Fatalf("没有收到 %s 事件", kind)
		}
	}
}

// wantJSON 检查值编码为 JSON 后包含指定的字段。
func wantJSON(t *testing.T, v any, keys ...string) {
	t.Helper()
	b, _ := json.Marshal(v)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Errorf("JSON %s 缺少字段 %q", b, k)
		}
	}
}

type fakeExplainer struct{ t *testing.T }

func (f fakeExplainer) Explain(ctx context.Context, runID string, m cabin.HoldMeta) (cabin.Explanation, error) {
	if runID == "" || m.Agent == "" || m.Host == "" {
		f.t.Errorf("Explain 收到的元数据不完整：%q %+v", runID, m)
	}
	return cabin.Explanation{What: "AI 解释", RiskLevel: "high", Source: "ai", PromptVersion: "fake"}, nil
}

type fakeInspector struct{}

func (fakeInspector) Entry(project string) ([]cabin.Finding, error) {
	return []cabin.Finding{{Path: ".env", Kind: cabin.Excluded, Reason: "含有密码"}}, nil
}

func (fakeInspector) Exit(files string, changes []staging.Change) []cabin.Finding {
	var out []cabin.Finding
	for _, c := range changes {
		if strings.HasPrefix(c.Path, ".git/hooks/") {
			out = append(out, cabin.Finding{Path: c.Path, Kind: cabin.HighRisk, Reason: "提交代码时会自动运行"})
		}
	}
	return out
}

type fakeSession struct{ bytes.Buffer }

func (*fakeSession) Close() error                { return nil }
func (*fakeSession) Resize(cols, rows int) error { return nil }

// fakeCore 是只有一个工作舱的假核心层。
type fakeCore struct {
	explainer cabin.Explainer
	inspector cabin.Inspector

	mu       sync.Mutex
	cabin    cabin.Cabin
	entry    []cabin.Finding
	changes  []staging.Change
	applied  map[string]bool
	log      []audit.Entry
	decision chan bool
	events   chan cabin.Event
}

func newFakeCore(e cabin.Explainer, i cabin.Inspector) *fakeCore {
	return &fakeCore{explainer: e, inspector: i, events: make(chan cabin.Event, 16)}
}

func (f *fakeCore) publish(e cabin.Event) { f.events <- e }

func (f *fakeCore) setStatus(s cabin.Status) {
	f.mu.Lock()
	f.cabin.Status = s
	f.mu.Unlock()
	f.publish(cabin.Event{Kind: "status", Cabin: f.cabin.ID, Status: s})
}

func (f *fakeCore) CheckDocker(ctx context.Context) error { return nil }

func (f *fakeCore) List() ([]cabin.Cabin, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []cabin.Cabin{f.cabin}, nil
}

func (f *fakeCore) Create(project, preset, tier string) (cabin.Cabin, error) {
	f.cabin = cabin.Cabin{ID: "c1", Name: "demo", Project: project, Preset: preset, Tier: tier, Status: cabin.Idle}
	return f.cabin, nil
}

// Start 按契约接上扩展和闸门：进舱检查，然后模拟闸门暂扣一个请求。
func (f *fakeCore) Start(ctx context.Context, id string) error {
	entry, err := f.inspector.Entry(f.cabin.Project)
	if err != nil {
		return err
	}
	f.entry, f.applied, f.decision = entry, map[string]bool{}, make(chan bool)
	f.cabin.RunID = "r1"
	f.setStatus(cabin.Running)

	cfg := gate.Config{Ask: f.ask, OnEvent: f.onEvent}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), gate.HoldTimeout)
		defer cancel()
		e := gate.Event{Time: time.Now(), Host: "evil.example", Port: 443, Method: "CONNECT", Rule: "hold"}
		e.Allowed = cfg.Ask(ctx, e)
		e.Rule = "user-deny"
		if e.Allowed {
			e.Rule = "user-allow"
		}
		cfg.OnEvent(e)
	}()
	return nil
}

func (f *fakeCore) ask(ctx context.Context, e gate.Event) bool {
	meta := cabin.HoldMeta{Agent: "Claude Code", Host: e.Host, Port: e.Port, Method: e.Method, Path: e.Path, Rule: e.Rule}
	h := cabin.Hold{ID: "h1", Cabin: f.cabin.ID, RunID: f.cabin.RunID, HoldMeta: meta, Explanation: cabin.Template(meta)}
	f.publish(cabin.Event{Kind: "hold", Cabin: h.Cabin, Hold: &h})
	go func() {
		ectx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		ex, err := f.explainer.Explain(ectx, h.RunID, meta)
		if errors.Is(err, cabin.ErrAdvisorOff) {
			return
		}
		if err != nil {
			ex = h.Explanation
			ex.AIUnavailable = true
		}
		h2 := h
		h2.Explanation = ex
		f.publish(cabin.Event{Kind: "explained", Cabin: h.Cabin, Hold: &h2})
	}()
	select {
	case allow := <-f.decision:
		f.publish(cabin.Event{Kind: "hold-closed", Cabin: h.Cabin, Hold: &h})
		return allow
	case <-ctx.Done():
		return false
	}
}

func (f *fakeCore) onEvent(e gate.Event) {
	f.mu.Lock()
	f.log = append(f.log, audit.Entry{Gate: &e})
	f.mu.Unlock()
	f.publish(cabin.Event{Kind: "request", Cabin: f.cabin.ID, Request: &e})
}

func (f *fakeCore) Stop(ctx context.Context, id string) error {
	f.changes = []staging.Change{
		{Path: "src/main.go", Kind: staging.Modified},
		{Path: ".git/hooks/pre-commit", Kind: staging.Added},
	}
	f.setStatus(cabin.Review)
	return nil
}

func (f *fakeCore) Reset(ctx context.Context, id string) error { return nil }

func (f *fakeCore) AgentURL(id string) (string, error) {
	return "http://127.0.0.1:9000/?cabin=" + id + "#token=fake-term-token", nil
}

func (f *fakeCore) OpenTerminal(id string) error { return nil }

func (f *fakeCore) Attach(ctx context.Context, id, token string) (sandbox.Session, error) {
	if token != "fake-term-token" {
		return nil, errors.New("终端令牌无效")
	}
	return &fakeSession{}, nil
}

func (f *fakeCore) EntryFindings(id string) ([]cabin.Finding, error) { return f.entry, nil }

func (f *fakeCore) ReviewItems(id string) ([]cabin.ReviewItem, error) {
	findings := f.inspector.Exit("files", f.changes)
	items := make([]cabin.ReviewItem, len(f.changes))
	for i, c := range f.changes {
		items[i].Change = c
		for _, fd := range findings {
			if fd.Path == c.Path {
				items[i].Findings = append(items[i].Findings, fd)
			}
		}
	}
	return items, nil
}

func (f *fakeCore) Apply(id, path string) error {
	f.applied[path] = true
	return nil
}

func (f *fakeCore) FinishReview(id string) error {
	f.mu.Lock()
	for _, c := range f.changes {
		f.log = append(f.log, audit.Entry{Review: &audit.Review{Time: time.Now(), Path: c.Path, Applied: f.applied[c.Path]}})
	}
	f.mu.Unlock()
	f.setStatus(cabin.Idle)
	return nil
}

// Report 在假核心层中直接汇总，代替尚未实现的 audit.Summarize。
func (f *fakeCore) Report(id string) (audit.Report, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rep := audit.Report{KeyUses: map[string]int{}}
	for _, e := range f.log {
		switch {
		case e.Gate != nil && strings.HasPrefix(e.Gate.Rule, "user-"):
			rep.Holds = append(rep.Holds, audit.HoldSummary{Host: e.Gate.Host, Rule: e.Gate.Rule})
		case e.Review != nil:
			rep.Files = append(rep.Files, *e.Review)
		}
	}
	return rep, nil
}

func (f *fakeCore) Decide(holdID string, allow bool) error {
	f.decision <- allow
	return nil
}

func (f *fakeCore) Subscribe() (<-chan cabin.Event, func()) { return f.events, func() {} }
