package gate

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// 测试在包内替换这两个变量，让隧道连到本地服务，并缩短暂扣时间。
var (
	dial        = (&net.Dialer{}).DialContext
	holdTimeout = HoldTimeout
)

// hold 是一个主机正在进行的暂扣。同一主机的并发请求等待同一个结果。
type hold struct {
	done    chan struct{}
	allowed bool
	timeout bool
}

func (g *Gate) connect(w http.ResponseWriter, r *http.Request) {
	host, p, err := net.SplitHostPort(r.Host)
	port, perr := strconv.Atoi(p)
	if err != nil || perr != nil || host == "" || port < 1 || port > 65535 {
		g.deny(w, Event{Host: r.Host, Method: r.Method}, "unsupported", http.StatusBadRequest)
		return
	}
	e := Event{Host: strings.TrimSuffix(strings.ToLower(host), "."), Port: port, Method: r.Method}
	rule, allowed := g.decide(e)
	if !allowed {
		g.deny(w, e, rule, http.StatusForbidden)
		return
	}
	e.Rule, e.Allowed = rule, true
	g.emit(e)
	g.tunnel(w, r, e)
}

// decide 按判定规则表给出 CONNECT 请求的规则和结果。
func (g *Gate) decide(e Event) (string, bool) {
	if e.Port == 443 && g.allow[e.Host] {
		return "tier", true
	}
	g.mu.Lock()
	if allowed, ok := g.decisions[e.Host]; ok {
		g.mu.Unlock()
		if allowed {
			return "run-allow", true
		}
		return "run-deny", false
	}
	h, waiting := g.holds[e.Host]
	if !waiting {
		h = &hold{done: make(chan struct{})}
		g.holds[e.Host] = h
	}
	g.mu.Unlock()

	if waiting {
		<-h.done
		switch {
		case h.timeout:
			return "timeout", false
		case h.allowed:
			return "run-allow", true
		}
		return "run-deny", false
	}

	ctx, cancel := context.WithTimeout(g.ctx, holdTimeout)
	held := e
	held.Rule, held.Allowed, held.Time = "hold", false, time.Now()
	allowed := g.cfg.Ask(ctx, held)
	// ctx 已经取消时，不管 Ask 返回什么，都按超时拒绝。
	h.timeout = ctx.Err() != nil
	h.allowed = allowed && !h.timeout
	cancel()

	g.mu.Lock()
	if !h.timeout {
		g.decisions[e.Host] = h.allowed
	}
	delete(g.holds, e.Host)
	g.mu.Unlock()
	close(h.done)

	switch {
	case h.timeout:
		return "timeout", false
	case h.allowed:
		return "user-allow", true
	}
	return "user-deny", false
}

// tunnel 连接目标主机，然后在沙箱和目标之间转发字节。闸门不解密内容。
func (g *Gate) tunnel(w http.ResponseWriter, r *http.Request, e Event) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	up, err := dial(ctx, "tcp", net.JoinHostPort(e.Host, strconv.Itoa(e.Port)))
	cancel()
	if err != nil {
		http.Error(w, "Airlock 闸门连不上目标主机", http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		up.Close()
		http.Error(w, "Airlock 闸门不支持这个连接", http.StatusInternalServerError)
		return
	}
	down, buf, err := hj.Hijack()
	if err != nil {
		up.Close()
		return
	}
	t := &tunnel{down: down, up: up, host: e.Host, user: e.Rule != "tier"}
	if !g.track(t) {
		t.close()
		return
	}
	defer g.untrack(t)
	if _, err := io.WriteString(down, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		t.close()
		return
	}
	pipe(down, buf.Reader, up)
}

// tunnel 是一条已建立的 CONNECT 隧道。
type tunnel struct {
	down, up net.Conn
	host     string
	user     bool // 由用户的决定放行，撤销决定时要断开
}

func (t *tunnel) close() {
	t.down.Close()
	t.up.Close()
}

// track 登记隧道。闸门已经关闭时返回 false。
func (g *Gate) track(t *tunnel) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false
	}
	g.tunnels[t] = struct{}{}
	return true
}

func (g *Gate) untrack(t *tunnel) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.tunnels, t)
}

// ResetDecision 撤销本次运行中对 host 的决定，并断开这个主机已建立的连接。
// 它只让下一个请求再次暂扣，不会放行任何请求。
func (g *Gate) ResetDecision(host string) {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	g.mu.Lock()
	defer g.mu.Unlock()
	allowed := g.decisions[host]
	delete(g.decisions, host)
	if !allowed {
		return
	}
	for t := range g.tunnels {
		if t.host == host && t.user {
			t.close()
		}
	}
}

// pipe 双向转发，任一方向结束时关闭两端。
func pipe(down net.Conn, downReader io.Reader, up net.Conn) {
	done := make(chan struct{}, 2)
	go func() { io.Copy(up, downReader); done <- struct{}{} }()
	go func() { io.Copy(down, up); done <- struct{}{} }()
	<-done
	down.Close()
	up.Close()
	<-done
}
