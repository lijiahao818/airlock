package gate

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// openConnect 发出一个 CONNECT，不读响应，让它一直挂着，直到测试结束。
func openConnect(t *testing.T, g *Gate, target string) {
	t.Helper()
	c, err := net.Dial("tcp", g.ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	io.WriteString(c, "CONNECT "+target+" HTTP/1.1\r\nHost: "+target+"\r\n\r\n")
}

// blockingAsker 的 Ask 一直等到 release 关闭，模拟用户还没有决定的暂扣。
func blockingAsker(t *testing.T) (*asker, func()) {
	release := make(chan struct{})
	a := &asker{answer: func(ctx context.Context) bool {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return false
	}}
	closed := false
	done := func() {
		if !closed {
			closed = true
			close(release)
		}
	}
	t.Cleanup(done)
	return a, done
}

func waitAsked(t *testing.T, a *asker, n int32) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for a.n.Load() < n {
		if time.Now().After(deadline) {
			t.Fatalf("Ask 只被调用了 %d 次，want %d", a.n.Load(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// 同时进行的 CONNECT 超过上限时，新的请求立即被拒绝，规则为 busy。连接数降下来后，新的请求照常处理（#84 AC-2）。
func TestConnectLimit(t *testing.T) {
	saved := maxConnects
	maxConnects = 3
	t.Cleanup(func() { maxConnects = saved })
	echoTarget(t)
	a, release := blockingAsker(t)
	g, rec := startGate(t, Config{Ask: a.ask})
	for i := range 3 {
		openConnect(t, g, fmt.Sprintf("fake-%d.example:443", i))
	}
	waitAsked(t, a, 3)

	start := time.Now()
	if code, _ := connectTo(t, g, "fake-extra.example:443"); code != http.StatusServiceUnavailable {
		t.Fatalf("超过上限：状态码 %d，want 503", code)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("超过上限的请求等了 %v 才被拒绝", d)
	}
	if e := rec.last(t); e.Rule != "busy" || e.Allowed || a.n.Load() != 3 {
		t.Fatalf("超过上限：事件 %+v，Ask %d 次", e, a.n.Load())
	}

	// 用户拒绝了等待中的暂扣，连接数降下来。
	release()
	deadline := time.Now().Add(5 * time.Second)
	for g.connects.Load() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("暂扣结束后仍有 %d 个连接", g.connects.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if code, _ := connectTo(t, g, "fake-extra.example:443"); code != http.StatusForbidden {
		t.Fatalf("连接数降下来后：状态码 %d，want 403", code)
	}
	if e := rec.last(t); e.Rule != "user-deny" || a.n.Load() != 4 {
		t.Fatalf("连接数降下来后没有再次暂扣：%+v，Ask %d 次", e, a.n.Load())
	}
}

// 300 个暂扣同时等待用户决定时，模型路由的请求仍然返回 200（#84 AC-1 的闸门部分）。
// 中继部分由 sandbox 包的 TestDockerRelayConcurrency 覆盖。
func TestRouteDuringManyHolds(t *testing.T) {
	echoTarget(t)
	up := &upstreamHits{}
	mockUpstream(t, up.handler)
	a, _ := blockingAsker(t)
	g, _ := startGate(t, Config{Providers: Providers(), Ask: a.ask})
	for i := range 300 {
		openConnect(t, g, fmt.Sprintf("fake-%d.example:443", i))
	}
	waitAsked(t, a, 300)
	if resp := post(t, g, "/anthropic/v1/messages", keyHeader(g, "anthropic")); resp.StatusCode != http.StatusOK {
		t.Fatalf("300 个暂扣等待时，模型请求的状态码 %d", resp.StatusCode)
	}
	if n := g.connects.Load(); n != 300 {
		t.Fatalf("正在进行的 CONNECT = %d，want 300", n)
	}
}
