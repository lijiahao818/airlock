package cabin

import (
	"context"
	"errors"
	"time"

	"github.com/lijiahao818/airlock/internal/audit"
	"github.com/lijiahao818/airlock/internal/gate"
)

// explainTimeout 后取消 Explainer.Explain 的 ctx。
const explainTimeout = 5 * time.Second

// pending 是一个等待用户决定的暂扣。
type pending struct {
	hold     Hold      // 除 Explanation 外创建后不再修改。Explanation 在 m.mu 下修改
	decision chan bool // 缓冲为 1，只由 Decide 写入
	closed   bool      // 用户已经决定，或暂扣已经结束。在 m.mu 下读写
}

// ask 返回传给 gate.Config.Ask 的函数。只有 Decide 能让它返回 true，解释的内容不影响结果。
func (m *Manager) ask(cabinID, runID, agent string) func(ctx context.Context, e gate.Event) bool {
	return func(ctx context.Context, e gate.Event) bool {
		meta := HoldMeta{Agent: agent, Host: e.Host, Port: e.Port, Method: e.Method, Path: e.Path, Rule: e.Rule}
		p := &pending{
			hold:     Hold{ID: newID(), Cabin: cabinID, RunID: runID, HoldMeta: meta, Explanation: Template(meta)},
			decision: make(chan bool, 1),
		}
		m.mu.Lock()
		m.holds[p.hold.ID] = p
		h := p.hold
		m.publish(Event{Kind: "hold", Cabin: cabinID, Hold: &h})
		m.mu.Unlock()
		// 先发出带模板解释的卡片，再异步生成 AI 解释，用户不用等。
		if m.o.Explainer != nil {
			go m.explain(ctx, p)
		}

		allow := false
		select {
		case allow = <-p.decision:
		case <-ctx.Done():
		}
		m.mu.Lock()
		p.closed = true
		delete(m.holds, p.hold.ID)
		h = p.hold
		m.publish(Event{Kind: "hold-closed", Cabin: cabinID, Hold: &h})
		m.mu.Unlock()
		return allow
	}
}

// explain 调用安全副驾，发出 explained 事件。用户已经做了决定时丢弃结果。
func (m *Manager) explain(ctx context.Context, p *pending) {
	ctx, cancel := context.WithTimeout(ctx, explainTimeout)
	defer cancel()
	ex, err := m.o.Explainer.Explain(ctx, p.hold.RunID, p.hold.HoldMeta)
	if errors.Is(err, ErrAdvisorOff) {
		return
	}
	if err != nil {
		ex = Template(p.hold.HoldMeta)
		ex.AIUnavailable = true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if p.closed {
		return
	}
	p.hold.Explanation = ex
	h := p.hold
	m.publish(Event{Kind: "explained", Cabin: h.Cabin, Hold: &h})
}

// Decide 只由控制台的用户点击触发。
func (m *Manager) Decide(holdID string, allow bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.holds[holdID]
	if p == nil || p.closed {
		return errors.New("cabin: 这个拦截已经处理过或已经超时。agent 再次连接时会出现新的拦截卡片。")
	}
	// closed 为 false 时还没有人写入，缓冲一定是空的。标记 closed 后，之后到达的 AI 解释被丢弃。
	p.decision <- allow
	p.closed = true
	return nil
}

// ResetDecision 撤销本次运行中对 host 的决定。host 的下一个请求再次暂扣，不会被放行。
func (m *Manager) ResetDecision(id, host string) error {
	if host == "" {
		return errors.New("cabin: 没有指定要撤销决定的地址。")
	}
	m.mu.Lock()
	r := m.runs[id]
	m.mu.Unlock()
	if r == nil {
		return errors.New("cabin: 工作舱没有在运行，没有可以撤销的决定。")
	}
	r.gate.ResetDecision(host)
	return nil
}

// onEvent 返回传给 gate.Config.OnEvent 的函数：写入审计记录，发出 request 事件。
func (m *Manager) onEvent(cabinID string, log *audit.Log) func(gate.Event) {
	return func(e gate.Event) {
		// 写入失败时只能丢失这一条记录，不影响闸门的判定。
		log.Add(audit.Entry{Gate: &e})
		m.mu.Lock()
		m.publish(Event{Kind: "request", Cabin: cabinID, Request: &e})
		m.mu.Unlock()
	}
}
