package main

// 本文件验证 docs/architecture.md 第 5 节的契约能否串起主要用户故事。
// 测试扮演控制器（console 的角色），fakeCore 扮演模型（核心层）。
// fakeCore 只用契约中的类型和回调搭建，不代表真实实现。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
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
	// F-5、ADR-008：复制项目文件时报告进度，复制完成后进入运行状态。
	started := collect(t, events, "progress", "status")
	if pg := started["progress"].Progress; pg == nil || pg.Total == 0 || pg.Done != pg.Total {
		t.Fatalf("progress = %+v", pg)
	}
	if started["status"].Status != cabin.Running {
		t.Fatalf("status = %q, want running", started["status"].Status)
	}

	// F-5：进舱检查排除 .env，依赖目录改用缓存卷。
	entry, _ := core.EntryFindings(c.ID)
	if len(entry) != 2 || entry[0].Kind != cabin.Excluded || entry[1].Kind != cabin.Cached || !strings.HasSuffix(entry[1].Path, "/") {
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
	if _, err := s.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	out := make([]byte, 2)
	if n, _ := s.Read(out); string(out[:n]) != "hi" {
		t.Fatalf("终端输出 = %q", out[:n])
	}
	// 断开只断开连接，用户刷新页面后可以重新连接同一个 agent。
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := core.Attach(ctx, c.ID, token); err != nil {
		t.Fatalf("断开后不能重新连接：%v", err)
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
	// 契约不规定 request 和 hold-closed 的先后，两者都必须收到。
	got := collect(t, events, "request", "hold-closed")
	if r := got["request"].Request; r.Allowed || r.Rule != "user-deny" {
		t.Fatalf("request = %+v", r)
	}
	if hc := got["hold-closed"].Hold; hc.ID != h.ID {
		t.Fatalf("hold-closed = %+v, want ID %q", hc, h.ID)
	}

	// F-7：用户撤销误点的拒绝。agent 重试时请求再次暂扣，用户这次允许。
	if err := core.ResetDecision(c.ID, h.Host); err != nil {
		t.Fatal(err)
	}
	h2 := next(t, events, "hold").Hold
	if h2.ID == h.ID || h2.Host != h.Host {
		t.Fatalf("撤销后的暂扣 = %+v", h2)
	}
	if err := core.Decide(h2.ID, true); err != nil {
		t.Fatal(err)
	}
	if r := collect(t, events, "request", "hold-closed")["request"].Request; !r.Allowed || r.Rule != "user-allow" {
		t.Fatalf("撤销后的 request = %+v", r)
	}

	// F-6、ADR-007：运行中预览改动，撤销高风险改动，agent 可以继续修改。运行中不能写回项目。
	items, _ := core.ReviewItems(c.ID)
	if len(items) != 2 || len(items[1].Findings) != 1 || items[1].Findings[0].Kind != cabin.HighRisk {
		t.Fatalf("运行中的 ReviewItems = %+v", items)
	}
	wantJSON(t, items[1], "path", "kind", "conflict", "special", "findings")
	if err := core.Apply(c.ID, items[0].Path); err == nil {
		t.Fatal("运行中不能把改动写回项目")
	}
	if err := core.Revert(c.ID, items[1].Path); err != nil {
		t.Fatal(err)
	}

	// F-8：停止运行后，剩下的改动完整，用户全部应用。停止后不能再撤销。
	if err := core.Stop(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	items, _ = core.ReviewItems(c.ID)
	if len(items) != 1 || len(items[0].Findings) != 0 {
		t.Fatalf("停止后的 ReviewItems = %+v", items)
	}
	if err := core.Revert(c.ID, items[0].Path); err == nil {
		t.Fatal("停止后不能再撤销")
	}
	for _, it := range items {
		if err := core.Apply(c.ID, it.Path); err != nil {
			t.Fatal(err)
		}
	}
	if err := core.FinishReview(c.ID); err != nil {
		t.Fatal(err)
	}

	// F-9：运行报告记录暂扣决定和文件决定。
	rep, _ := core.Report(c.ID)
	if len(rep.Holds) != 2 || rep.Holds[0].Rule != "user-deny" || rep.Holds[1].Rule != "user-allow" {
		t.Fatalf("Holds = %+v", rep.Holds)
	}
	if len(rep.Files) != 1 || !rep.Files[0].Applied {
		t.Fatalf("Files = %+v", rep.Files)
	}
	wantJSON(t, rep, "hosts", "holds", "keyUses", "files")
	list, _ := core.List()
	if list[0].Status != cabin.Idle {
		t.Fatalf("status = %q, want idle", list[0].Status)
	}
}

// next 读取事件，直到遇到指定类型。kind 为空时返回下一个事件。
func next(t *testing.T, ch <-chan cabin.Event, kind string) cabin.Event {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				t.Fatalf("事件通道已关闭，没有收到 %s 事件", kind)
			}
			if kind == "" || e.Kind == kind {
				return e
			}
		case <-timeout:
			t.Fatalf("没有收到 %s 事件", kind)
		}
	}
}

// collect 读取事件，直到每种指定类型都收到一次。
func collect(t *testing.T, ch <-chan cabin.Event, kinds ...string) map[string]cabin.Event {
	t.Helper()
	got := map[string]cabin.Event{}
	for len(got) < len(kinds) {
		e := next(t, ch, "")
		for _, k := range kinds {
			if e.Kind == k {
				got[k] = e
			}
		}
	}
	return got
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
	return []cabin.Finding{
		{Path: ".env", Kind: cabin.Excluded, Reason: "含有密码"},
		{Path: "node_modules/", Kind: cabin.Cached, Reason: "依赖目录，agent 在沙箱中重新安装"},
	}, nil
}

// Rewrite 不改写任何文件。
func (fakeInspector) Rewrite(rel string) func(content []byte) []byte { return nil }

func (fakeInspector) Exit(files string, changes []staging.Change) []cabin.Finding {
	var out []cabin.Finding
	for _, c := range changes {
		switch {
		case strings.HasPrefix(c.Path, ".git/hooks/"):
			out = append(out, cabin.Finding{Path: c.Path, Kind: cabin.HighRisk, Reason: "提交代码时会自动运行"})
		case strings.HasPrefix(c.Path, "node_modules/"):
			out = append(out, cabin.Finding{Path: c.Path, Kind: cabin.Cached, Reason: "依赖目录"})
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
	holds    int
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
	for i := 1; i <= 2; i++ {
		f.publish(cabin.Event{Kind: "progress", Cabin: f.cabin.ID, Progress: &cabin.Progress{Done: i, Total: 2}})
	}
	// 模拟 agent 在运行中改了两个文件，并安装了一个依赖。
	f.changes = []staging.Change{
		{Path: "src/main.go", Kind: staging.Modified},
		{Path: ".git/hooks/pre-commit", Kind: staging.Added},
		{Path: "node_modules/left-pad/index.js", Kind: staging.Added},
	}
	f.cabin.RunID = "r1"
	f.setStatus(cabin.Running)

	go f.agentRequest("evil.example")
	return nil
}

// agentRequest 模拟 agent 访问档位以外的主机：闸门按契约调用 Ask 和 OnEvent。
func (f *fakeCore) agentRequest(host string) {
	cfg := gate.Config{Ask: f.ask, OnEvent: f.onEvent}
	ctx, cancel := context.WithTimeout(context.Background(), gate.HoldTimeout)
	defer cancel()
	e := gate.Event{Time: time.Now(), Host: host, Port: 443, Method: "CONNECT", Rule: "hold"}
	e.Allowed = cfg.Ask(ctx, e)
	e.Rule = "user-deny"
	if e.Allowed {
		e.Rule = "user-allow"
	}
	cfg.OnEvent(e)
}

// ResetDecision 撤销决定后，模拟 agent 重试同一个主机，请求再次暂扣。
func (f *fakeCore) ResetDecision(id, host string) error {
	if f.status() != cabin.Running {
		return errors.New("只在运行中可以撤销决定")
	}
	go f.agentRequest(host)
	return nil
}

func (f *fakeCore) ask(ctx context.Context, e gate.Event) bool {
	meta := cabin.HoldMeta{Agent: "Claude Code", Host: e.Host, Port: e.Port, Method: e.Method, Path: e.Path, Rule: e.Rule}
	f.mu.Lock()
	f.holds++
	id := fmt.Sprintf("h%d", f.holds)
	f.mu.Unlock()
	h := cabin.Hold{ID: id, Cabin: f.cabin.ID, RunID: f.cabin.RunID, HoldMeta: meta, Explanation: cabin.Template(meta)}
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
	f.setStatus(cabin.Review)
	return nil
}

func (f *fakeCore) status() cabin.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cabin.Status
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
	var items []cabin.ReviewItem
	for _, c := range f.changes {
		it := cabin.ReviewItem{Change: c}
		for _, fd := range findings {
			if fd.Path == c.Path {
				it.Findings = append(it.Findings, fd)
			}
		}
		// 依赖目录的改动不进入审查。
		if !slices.ContainsFunc(it.Findings, func(fd cabin.Finding) bool { return fd.Kind == cabin.Cached }) {
			items = append(items, it)
		}
	}
	return items, nil
}

func (f *fakeCore) Revert(id, path string) error {
	if f.status() != cabin.Running {
		return errors.New("只在运行中可以撤销")
	}
	f.changes = slices.DeleteFunc(f.changes, func(c staging.Change) bool { return c.Path == path })
	return nil
}

func (f *fakeCore) Apply(id, path string) error {
	if f.status() != cabin.Review {
		return errors.New("只在运行停止后可以写回项目")
	}
	f.applied[path] = true
	return nil
}

// FinishReview 只记录进入审查的改动，依赖目录的改动不记录。
func (f *fakeCore) FinishReview(id string) error {
	items, _ := f.ReviewItems(id)
	f.mu.Lock()
	for _, it := range items {
		f.log = append(f.log, audit.Entry{Review: &audit.Review{Time: time.Now(), Path: it.Path, Applied: f.applied[it.Path]}})
	}
	f.mu.Unlock()
	f.setStatus(cabin.Idle)
	return nil
}

func (f *fakeCore) Report(id string) (audit.Report, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return audit.Summarize(f.log), nil
}

func (f *fakeCore) Decide(holdID string, allow bool) error {
	select {
	case f.decision <- allow:
		return nil
	case <-time.After(time.Second):
		return errors.New("没有待处理的暂扣")
	}
}

func (f *fakeCore) Subscribe() (<-chan cabin.Event, func()) { return f.events, func() {} }
