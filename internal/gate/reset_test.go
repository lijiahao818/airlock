package gate

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// closedSoon 检查连接在短时间内被闸门关闭。
func closedSoon(c net.Conn) bool {
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err := c.Read(make([]byte, 1))
	return err == io.EOF
}

// stillOpen 检查隧道仍然能转发数据。
func stillOpen(c net.Conn) bool {
	c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.WriteString(c, "x"); err != nil {
		return false
	}
	_, err := c.Read(make([]byte, 1))
	return err == nil
}

func TestResetDenyAsksAgain(t *testing.T) {
	echoTarget(t)
	var allow atomic.Bool
	a := &asker{answer: func(context.Context) bool { return allow.Load() }}
	g, rec := startGate(t, Config{Ask: a.ask})
	connectTo(t, g, "fake-site.example:443")
	g.ResetDecision("FAKE-SITE.example.:443")
	allow.Store(true)
	if code, _ := connectTo(t, g, "fake-site.example:443"); code != http.StatusOK {
		t.Fatalf("撤销拒绝后再允许：状态码 %d", code)
	}
	if e := rec.last(t); e.Rule != "user-allow" || a.n.Load() != 2 {
		t.Errorf("撤销后没有再次暂扣：%+v，Ask %d 次", e, a.n.Load())
	}
}

func TestResetAllowClosesTunnels(t *testing.T) {
	echoTarget(t)
	var allow atomic.Bool
	allow.Store(true)
	a := &asker{answer: func(context.Context) bool { return allow.Load() }}
	g, rec := startGate(t, Config{Allow: []string{"fake-pkg.example"}, Ask: a.ask})
	_, site := connectTo(t, g, "fake-site.example:443")
	_, other := connectTo(t, g, "fake-other.example:443")
	_, pkg := connectTo(t, g, "fake-pkg.example:443")

	g.ResetDecision("fake-site.example:443")
	if !closedSoon(site) {
		t.Error("撤销允许后，这个主机的隧道没有断开")
	}
	if !stillOpen(other) || !stillOpen(pkg) {
		t.Error("撤销一个主机的决定时，断开了其他隧道")
	}
	// 撤销只让请求再次暂扣，不会放行。
	allow.Store(false)
	if code, _ := connectTo(t, g, "fake-site.example:443"); code != http.StatusForbidden {
		t.Errorf("撤销后用户拒绝：状态码 %d", code)
	}
	if e := rec.last(t); e.Rule != "user-deny" {
		t.Errorf("撤销后的事件 %+v", e)
	}
}

// 连接目标主机期间撤销决定时，正在建立的隧道也不能保留。
func TestResetDuringDial(t *testing.T) {
	echoTarget(t)
	a := &asker{answer: answer(true)}
	g, _ := startGate(t, Config{Ask: a.ask})
	connectTo(t, g, "fake-site.example:443")

	saved := dial
	dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		g.ResetDecision("fake-site.example:443")
		return saved(ctx, network, addr)
	}
	c, err := net.Dial("tcp", g.ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	io.WriteString(c, "CONNECT fake-site.example:443 HTTP/1.1\r\nHost: fake-site.example:443\r\n\r\n")
	if !closedSoon(c) {
		t.Fatal("连接期间撤销了决定，隧道仍然建立")
	}
}

// 档位内的主机没有用户决定，撤销不影响它的隧道。
func TestResetKeepsTierTunnel(t *testing.T) {
	echoTarget(t)
	g, _ := startGate(t, Config{Allow: []string{"fake-pkg.example"}})
	_, pkg := connectTo(t, g, "fake-pkg.example:443")
	g.ResetDecision("fake-pkg.example:443")
	g.ResetDecision("fake-never-seen.example:443")
	if !stillOpen(pkg) {
		t.Fatal("撤销断开了档位内主机的隧道")
	}
}

func TestCloseClosesTunnels(t *testing.T) {
	echoTarget(t)
	g, _ := startGate(t, Config{Allow: []string{"fake-pkg.example"}})
	_, pkg := connectTo(t, g, "fake-pkg.example:443")
	g.Close()
	if !closedSoon(pkg) {
		t.Fatal("Close 之后隧道没有断开")
	}
}
