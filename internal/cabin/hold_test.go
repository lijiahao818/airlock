package cabin

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lijiahao818/airlock/internal/audit"
	"github.com/lijiahao818/airlock/internal/gate"
)

var heldEvent = gate.Event{Host: "evil.example", Port: 443, Method: "CONNECT", Rule: "hold"}

// hold 在后台调用 ask，返回 ask 的结果和 hold 事件。
func (e *env) hold(t *testing.T, ctx context.Context) (<-chan bool, Hold) {
	t.Helper()
	res := make(chan bool, 1)
	ask := e.m.ask("c1", "r1", "Claude Code")
	go func() { res <- ask(ctx, heldEvent) }()
	return res, *e.next(t, "hold").Hold
}

func result(t *testing.T, res <-chan bool) bool {
	t.Helper()
	select {
	case v := <-res:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("Ask 没有返回")
		return false
	}
}

func TestAskReturnsUserDecision(t *testing.T) {
	for _, allow := range []bool{true, false} {
		e := newEnv(t, nil, nil)
		res, h := e.hold(t, context.Background())
		want := HoldMeta{Agent: "Claude Code", Host: "evil.example", Port: 443, Method: "CONNECT", Rule: "hold"}
		if h.HoldMeta != want || h.Cabin != "c1" || h.RunID != "r1" || h.ID == "" {
			t.Fatalf("hold = %+v", h)
		}
		if h.Explanation != Template(want) {
			t.Fatalf("hold 事件应该带模板解释：%+v", h.Explanation)
		}
		if err := e.m.Decide(h.ID, allow); err != nil {
			t.Fatal(err)
		}
		if got := result(t, res); got != allow {
			t.Fatalf("Ask = %v, want %v", got, allow)
		}
		if closed := e.next(t, "hold-closed").Hold; closed.ID != h.ID {
			t.Fatalf("hold-closed = %+v", closed)
		}
		// 已经处理过的暂扣不能再决定。
		if err := e.m.Decide(h.ID, true); err == nil {
			t.Fatal("重复的 Decide 应该返回错误")
		}
	}
}

func TestAskReturnsFalseWhenContextEnds(t *testing.T) {
	e := newEnv(t, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	res, h := e.hold(t, ctx)
	cancel()
	if result(t, res) {
		t.Fatal("ctx 结束时 Ask 必须返回 false")
	}
	e.next(t, "hold-closed")
	if err := e.m.Decide(h.ID, true); err == nil {
		t.Fatal("超时的暂扣不能再决定")
	}
}

func TestDecideUnknownHold(t *testing.T) {
	e := newEnv(t, nil, nil)
	if err := e.m.Decide("no-such-hold", true); err == nil {
		t.Fatal("未知的暂扣应该返回错误")
	}
}

// AC-7：先发出带模板解释的 hold 事件，再调用 Explain，5 秒后取消 ctx。
func TestExplainAfterHoldWithDeadline(t *testing.T) {
	type call struct {
		holdFirst bool
		left      time.Duration
		meta      HoldMeta
		runID     string
	}
	calls := make(chan call, 1)
	// probe 是单独的订阅，测试不从中读取，所以 Explain 被调用时可以看到 hold 事件是否已经发出。
	var probe <-chan Event
	e := newEnv(t, fakeExplainer(func(ctx context.Context, runID string, m HoldMeta) (Explanation, error) {
		dl, _ := ctx.Deadline()
		calls <- call{holdFirst: len(probe) > 0, left: time.Until(dl), meta: m, runID: runID}
		return Explanation{What: "AI 解释", Risk: "高", RiskLevel: "high", Action: "拒绝", Source: "ai", PromptVersion: "fake-v1"}, nil
	}), nil)
	probe, cancel := e.m.Subscribe()
	defer cancel()
	res, h := e.hold(t, context.Background())
	c := <-calls
	if !c.holdFirst {
		t.Fatal("调用 Explain 时还没有发出 hold 事件")
	}
	if c.left <= 4*time.Second || c.left > explainTimeout {
		t.Fatalf("Explain 的 ctx 应该在 5 秒后取消，剩余 %v", c.left)
	}
	if c.meta != h.HoldMeta || c.runID != "r1" {
		t.Fatalf("Explain 收到 %q %+v", c.runID, c.meta)
	}
	ex := e.next(t, "explained").Hold
	if ex.ID != h.ID || ex.Explanation.Source != "ai" || ex.Explanation.PromptVersion != "fake-v1" {
		t.Fatalf("explained = %+v", ex)
	}
	e.m.Decide(h.ID, false)
	result(t, res)
}

// AC-8：安全副驾关闭时不发出 explained 事件。其他错误时发出模板解释，并标明 AI 解释暂不可用。
func TestExplainErrors(t *testing.T) {
	t.Run("安全副驾关闭", func(t *testing.T) {
		done := make(chan struct{})
		e := newEnv(t, fakeExplainer(func(context.Context, string, HoldMeta) (Explanation, error) {
			defer close(done)
			return Explanation{}, ErrAdvisorOff
		}), nil)
		res, h := e.hold(t, context.Background())
		<-done
		e.none(t, "explained", 200*time.Millisecond)
		e.m.Decide(h.ID, false)
		result(t, res)
	})
	t.Run("调用失败", func(t *testing.T) {
		e := newEnv(t, fakeExplainer(func(context.Context, string, HoldMeta) (Explanation, error) {
			return Explanation{}, errors.New("fake: 超时")
		}), nil)
		res, h := e.hold(t, context.Background())
		ex := e.next(t, "explained").Hold.Explanation
		want := Template(h.HoldMeta)
		want.AIUnavailable = true
		if ex != want {
			t.Fatalf("explained = %+v, want %+v", ex, want)
		}
		e.m.Decide(h.ID, false)
		result(t, res)
	})
}

// AC-9：用户在 AI 解释生成前做了决定时，不再发出 explained 事件。
func TestExplanationAfterDecisionIsDropped(t *testing.T) {
	release, returned := make(chan struct{}), make(chan struct{})
	e := newEnv(t, fakeExplainer(func(context.Context, string, HoldMeta) (Explanation, error) {
		defer close(returned)
		<-release
		return Explanation{What: "迟到的解释", Source: "ai"}, nil
	}), nil)
	res, h := e.hold(t, context.Background())
	if err := e.m.Decide(h.ID, false); err != nil {
		t.Fatal(err)
	}
	result(t, res)
	e.next(t, "hold-closed")
	close(release)
	<-returned
	e.none(t, "explained", 200*time.Millisecond)
}

// AC-10：安全副驾建议放行时，请求仍然暂扣。只有 Decide 能放行。
func TestSuggestAllowDoesNotAllow(t *testing.T) {
	e := newEnv(t, fakeExplainer(func(context.Context, string, HoldMeta) (Explanation, error) {
		return Explanation{What: "安全", RiskLevel: "low", SuggestAllow: true, Source: "ai"}, nil
	}), nil)
	res, h := e.hold(t, context.Background())
	if ex := e.next(t, "explained").Hold.Explanation; !ex.SuggestAllow {
		t.Fatalf("explained = %+v", ex)
	}
	select {
	case v := <-res:
		t.Fatalf("没有 Decide 时 Ask 返回了 %v", v)
	case <-time.After(300 * time.Millisecond):
	}
	e.m.Decide(h.ID, false)
	if result(t, res) {
		t.Fatal("用户拒绝时 Ask 必须返回 false")
	}
}

func TestOnEventWritesAuditAndPublishes(t *testing.T) {
	e := newEnv(t, nil, nil)
	path := filepath.Join(t.TempDir(), "run.jsonl")
	log, err := audit.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ev := gate.Event{Host: "api.anthropic.com", Port: 443, Method: "POST", Path: "/anthropic/v1/messages", Rule: "route", Allowed: true, Provider: "anthropic"}
	e.m.onEvent("c1", log)(ev)
	log.Close()
	if got := e.next(t, "request"); got.Cabin != "c1" || *got.Request != ev {
		t.Fatalf("request = %+v", got)
	}
	entries, err := audit.Read(path)
	if err != nil || len(entries) != 1 || *entries[0].Gate != ev {
		t.Fatalf("审计记录 = %+v, %v", entries, err)
	}
}

func TestResetDecisionNeedsRunningCabin(t *testing.T) {
	e := newEnv(t, nil, nil)
	c := e.create(t, "claude-code")
	if err := e.m.ResetDecision(c.ID, "evil.example"); err == nil {
		t.Fatal("空闲的工作舱不能撤销决定")
	}
	if err := e.m.ResetDecision(c.ID, ""); err == nil {
		t.Fatal("没有主机时应该返回错误")
	}
}
