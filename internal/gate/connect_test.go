package gate

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// echoTarget 把隧道的全部出站连接接到本地的回显服务，并记录目标地址。
func echoTarget(t *testing.T) *[]string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { io.Copy(c, c); c.Close() }()
		}
	}()
	var mu sync.Mutex
	addrs := &[]string{}
	saved := dial
	dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		mu.Lock()
		*addrs = append(*addrs, addr)
		mu.Unlock()
		return (&net.Dialer{}).DialContext(ctx, network, ln.Addr().String())
	}
	t.Cleanup(func() { dial = saved })
	return addrs
}

// connectTo 发送 CONNECT 请求。状态码为 200 时，检查隧道能双向转发，并返回打开的连接。
func connectTo(t *testing.T, g *Gate, target string) (int, net.Conn) {
	t.Helper()
	c, err := net.Dial("tcp", g.ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(c, "CONNECT "+target+" HTTP/1.1\r\nHost: "+target+"\r\n\r\n")
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		c.Close()
		t.Fatalf("CONNECT %s：%v", target, err)
	}
	if resp.StatusCode != http.StatusOK {
		c.Close()
		return resp.StatusCode, nil
	}
	io.WriteString(c, "fake-ping")
	buf := make([]byte, len("fake-ping"))
	if _, err := io.ReadFull(br, buf); err != nil || string(buf) != "fake-ping" {
		t.Fatalf("隧道没有转发数据：%q, %v", buf, err)
	}
	t.Cleanup(func() { c.Close() })
	return resp.StatusCode, c
}

// asker 记录 Ask 收到的事件，并按 answer 返回。
type asker struct {
	n      atomic.Int32
	mu     sync.Mutex
	held   []Event
	answer func(ctx context.Context) bool
}

func (a *asker) ask(ctx context.Context, e Event) bool {
	a.n.Add(1)
	a.mu.Lock()
	a.held = append(a.held, e)
	a.mu.Unlock()
	return a.answer(ctx)
}

func answer(v bool) func(context.Context) bool { return func(context.Context) bool { return v } }

func TestConnectTier(t *testing.T) {
	addrs := echoTarget(t)
	a := &asker{answer: answer(false)}
	g, rec := startGate(t, Config{Allow: []string{"fake-pkg.example"}, Ask: a.ask})
	if code, _ := connectTo(t, g, "FAKE-PKG.example.:443"); code != http.StatusOK {
		t.Fatalf("档位内的主机：状态码 %d", code)
	}
	if e := rec.last(t); e.Rule != "tier" || !e.Allowed || e.Host != "fake-pkg.example" || e.Port != 443 || e.Method != "CONNECT" {
		t.Errorf("事件 %+v", e)
	}
	if (*addrs)[0] != "fake-pkg.example:443" {
		t.Errorf("隧道连接了 %s", (*addrs)[0])
	}
	if a.n.Load() != 0 {
		t.Error("档位内的主机被暂扣")
	}
	// 档位只放行 443 端口。
	if code, _ := connectTo(t, g, "fake-pkg.example:22"); code != http.StatusForbidden {
		t.Errorf("档位内主机的 22 端口：状态码 %d，want 403", code)
	}
	if a.n.Load() != 1 {
		t.Error("档位内主机的其他端口没有暂扣")
	}
}

func TestConnectUserAllow(t *testing.T) {
	echoTarget(t)
	a := &asker{answer: answer(true)}
	g, rec := startGate(t, Config{Ask: a.ask})
	connectTo(t, g, "fake-site.example:443")
	connectTo(t, g, "fake-site.example:443")
	if got, want := rec.rules(), []string{"user-allow", "run-allow"}; !slices.Equal(got, want) {
		t.Errorf("规则 %v，want %v", got, want)
	}
	if a.n.Load() != 1 {
		t.Errorf("Ask 被调用 %d 次，want 1", a.n.Load())
	}
	if e := a.held[0]; e.Rule != "hold" || e.Allowed || e.Host != "fake-site.example" || e.Time.IsZero() {
		t.Errorf("Ask 收到的事件 %+v", e)
	}
}

func TestConnectUserDeny(t *testing.T) {
	addrs := echoTarget(t)
	a := &asker{answer: answer(false)}
	g, rec := startGate(t, Config{Ask: a.ask})
	for range 2 {
		if code, _ := connectTo(t, g, "fake-evil.example:443"); code != http.StatusForbidden {
			t.Errorf("状态码 %d，want 403", code)
		}
	}
	if got, want := rec.rules(), []string{"user-deny", "run-deny"}; !slices.Equal(got, want) {
		t.Errorf("规则 %v，want %v", got, want)
	}
	if a.n.Load() != 1 || len(*addrs) != 0 {
		t.Errorf("Ask 被调用 %d 次，连接了 %v", a.n.Load(), *addrs)
	}
}

func TestConnectTimeout(t *testing.T) {
	echoTarget(t)
	saved := holdTimeout
	holdTimeout = 50 * time.Millisecond
	t.Cleanup(func() { holdTimeout = saved })
	// 超时后才返回 true，也必须按超时拒绝。
	a := &asker{answer: func(ctx context.Context) bool { <-ctx.Done(); return true }}
	g, rec := startGate(t, Config{Ask: a.ask})
	for range 2 {
		if code, _ := connectTo(t, g, "fake-slow.example:443"); code != http.StatusForbidden {
			t.Errorf("状态码 %d，want 403", code)
		}
	}
	if got, want := rec.rules(), []string{"timeout", "timeout"}; !slices.Equal(got, want) {
		t.Errorf("规则 %v，want %v", got, want)
	}
	if a.n.Load() != 2 {
		t.Errorf("超时后再次请求，Ask 被调用 %d 次，want 2", a.n.Load())
	}
}

func TestConnectConcurrentHoldAsksOnce(t *testing.T) {
	echoTarget(t)
	release := make(chan struct{})
	a := &asker{answer: func(context.Context) bool { <-release; return true }}
	g, rec := startGate(t, Config{Ask: a.ask})
	var wg sync.WaitGroup
	codes := make([]int, 5)
	for i := range codes {
		wg.Go(func() { codes[i], _ = connectTo(t, g, "fake-site.example:443") })
	}
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()
	for i, code := range codes {
		if code != http.StatusOK {
			t.Errorf("第 %d 个请求：状态码 %d", i, code)
		}
	}
	if a.n.Load() != 1 {
		t.Errorf("Ask 被调用 %d 次，want 1", a.n.Load())
	}
	if n := len(rec.rules()); n != 5 {
		t.Errorf("OnEvent 被调用 %d 次，want 5", n)
	}
}

func TestCloseEndsHold(t *testing.T) {
	echoTarget(t)
	asked := make(chan struct{})
	a := &asker{answer: func(ctx context.Context) bool { close(asked); <-ctx.Done(); return false }}
	g, _ := startGate(t, Config{Ask: a.ask})
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := net.Dial("tcp", g.ln.Addr().String())
		if err != nil {
			return
		}
		defer c.Close()
		io.WriteString(c, "CONNECT fake-site.example:443 HTTP/1.1\r\nHost: fake-site.example:443\r\n\r\n")
		c.Read(make([]byte, 1))
	}()
	<-asked
	g.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close 之后暂扣没有结束")
	}
}

func TestNewGateForgetsDecisions(t *testing.T) {
	echoTarget(t)
	a := &asker{answer: answer(false)}
	g, _ := startGate(t, Config{Ask: a.ask})
	connectTo(t, g, "fake-site.example:443")
	g.Close()
	g2, rec := startGate(t, Config{Ask: a.ask})
	connectTo(t, g2, "fake-site.example:443")
	if e := rec.last(t); e.Rule != "user-deny" || a.n.Load() != 2 {
		t.Errorf("新实例继承了旧实例的决定：%+v", e)
	}
}

func TestConnectRejectsBadTarget(t *testing.T) {
	addrs := echoTarget(t)
	a := &asker{answer: answer(true)}
	g, rec := startGate(t, Config{Ask: a.ask})
	for _, target := range []string{"fake-noport.example", ":443", "fake-site.example:0", "fake-site.example:99999", "fake-site.example:http"} {
		if code, _ := connectTo(t, g, target); code != http.StatusBadRequest {
			t.Errorf("%s：状态码 %d，want 400", target, code)
		}
		if e := rec.last(t); e.Rule != "unsupported" || e.Allowed {
			t.Errorf("%s：事件 %+v", target, e)
		}
	}
	if a.n.Load() != 0 || len(*addrs) != 0 {
		t.Error("不合法的目标被暂扣或连接")
	}
}

func TestConnectDialFails(t *testing.T) {
	saved := dial
	dial = func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("fake-unreachable") }
	t.Cleanup(func() { dial = saved })
	g, _ := startGate(t, Config{Allow: []string{"fake-pkg.example"}})
	if code, _ := connectTo(t, g, "fake-pkg.example:443"); code != http.StatusBadGateway {
		t.Fatalf("状态码 %d，want 502", code)
	}
}
