package gate

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
)

// 允许一个端口不放行同一主机的其他端口。用户以为只允许了 HTTPS，agent 不能借此连接 SSH 或 SMTP（#73）。
func TestAllowIsPerPort(t *testing.T) {
	echoTarget(t)
	var allow atomic.Bool
	allow.Store(true)
	a := &asker{answer: func(context.Context) bool { return allow.Load() }}
	g, rec := startGate(t, Config{Ask: a.ask})
	if code, _ := connectTo(t, g, "fake-site.example:443"); code != http.StatusOK {
		t.Fatalf("允许 443：状态码 %d", code)
	}
	allow.Store(false)
	if code, _ := connectTo(t, g, "fake-site.example:22"); code != http.StatusForbidden {
		t.Fatalf("22 端口：状态码 %d，want 403", code)
	}
	if e := rec.last(t); e.Rule != "user-deny" || e.Port != 22 || a.n.Load() != 2 {
		t.Fatalf("22 端口没有再次暂扣：%+v，Ask %d 次", e, a.n.Load())
	}
	// 443 端口的允许仍然有效。
	if code, _ := connectTo(t, g, "fake-site.example:443"); code != http.StatusOK {
		t.Fatalf("再次连接 443：状态码 %d", code)
	}
	if e := rec.last(t); e.Rule != "run-allow" || a.n.Load() != 2 {
		t.Fatalf("再次连接 443：%+v，Ask %d 次", e, a.n.Load())
	}
}

// 拒绝一个端口不直接拒绝同一主机的其他端口，那些端口再次暂扣。
func TestDenyIsPerPort(t *testing.T) {
	echoTarget(t)
	a := &asker{answer: answer(false)}
	g, rec := startGate(t, Config{Ask: a.ask})
	connectTo(t, g, "fake-site.example:443")
	if code, _ := connectTo(t, g, "fake-site.example:8443"); code != http.StatusForbidden {
		t.Fatalf("8443 端口：状态码 %d", code)
	}
	if e := rec.last(t); e.Rule != "user-deny" || e.Port != 8443 || a.n.Load() != 2 {
		t.Fatalf("8443 端口没有再次暂扣：%+v，Ask %d 次", e, a.n.Load())
	}
	connectTo(t, g, "fake-site.example:443")
	if e := rec.last(t); e.Rule != "run-deny" || a.n.Load() != 2 {
		t.Fatalf("再次连接 443：%+v，Ask %d 次", e, a.n.Load())
	}
}

// 撤销一个端口的允许，只断开这个端口的隧道，同一主机其他端口的隧道保留。
func TestResetIsPerPort(t *testing.T) {
	echoTarget(t)
	a := &asker{answer: answer(true)}
	g, _ := startGate(t, Config{Ask: a.ask})
	_, https := connectTo(t, g, "fake-site.example:443")
	_, alt := connectTo(t, g, "fake-site.example:8443")
	g.ResetDecision("fake-site.example:443")
	if !closedSoon(https) {
		t.Error("撤销 443 后，443 的隧道没有断开")
	}
	if !stillOpen(alt) {
		t.Error("撤销 443 时，断开了 8443 的隧道")
	}
	// 8443 的允许仍然有效，不再询问。
	connectTo(t, g, "fake-site.example:8443")
	if a.n.Load() != 2 {
		t.Errorf("撤销 443 影响了 8443 的决定：Ask %d 次", a.n.Load())
	}
}

// 格式不对的参数什么也不撤销。
func TestResetRejectsBadTarget(t *testing.T) {
	echoTarget(t)
	a := &asker{answer: answer(true)}
	g, _ := startGate(t, Config{Ask: a.ask})
	_, c := connectTo(t, g, "fake-site.example:443")
	for _, bad := range []string{"fake-site.example", "fake-site.example:https", ""} {
		g.ResetDecision(bad)
	}
	if !stillOpen(c) {
		t.Fatal("格式不对的参数断开了隧道")
	}
	connectTo(t, g, "fake-site.example:443")
	if a.n.Load() != 1 {
		t.Fatalf("格式不对的参数撤销了决定：Ask %d 次", a.n.Load())
	}
}
