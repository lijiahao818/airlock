package main

import (
	"context"
	"net"
	"net/http"
	"regexp"
	"testing"

	"github.com/lijiahao818/airlock/internal/sandbox"
)

// fakeBackend 只实现 cabin.New 调用的 Prune。组装时不调用其他方法，调用时测试会 panic。
type fakeBackend struct{ sandbox.Backend }

func (fakeBackend) Prune(context.Context) error { return nil }

func assembleForTest(t *testing.T) *app {
	t.Helper()
	a, err := assemble(t.TempDir(), fakeBackend{}, fakeSecret)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.close)
	go http.Serve(a.consoleLn, a.console)
	go http.Serve(a.termLn, a.term)
	return a
}

func TestAssemblePorts(t *testing.T) {
	a := assembleForTest(t)
	c := a.consoleLn.Addr().(*net.TCPAddr)
	w := a.termLn.Addr().(*net.TCPAddr)
	if !c.IP.Equal(net.IPv4(127, 0, 0, 1)) || !w.IP.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("监听地址不是 127.0.0.1：控制台 %s，网页终端 %s", c, w)
	}
	if c.Port == w.Port {
		t.Fatalf("控制台和网页终端使用同一个端口 %d", c.Port)
	}
}

func TestAssembleURL(t *testing.T) {
	a := assembleForTest(t)
	// 令牌格式与 web/src/api/token.ts 的 tokenRe 一致，否则前端不接受。
	re := regexp.MustCompile(`^http://127\.0\.0\.1:(\d+)/#token=([A-Za-z0-9_-]{16,256})$`)
	m := re.FindStringSubmatch(a.url())
	if m == nil {
		t.Fatalf("网址格式不对：%s", a.url())
	}
	if m[2] != a.token {
		t.Fatal("网址中的令牌不是控制台的令牌")
	}
	if b := assembleForTest(t); b.token == a.token {
		t.Fatal("两次启动生成了同一个令牌")
	}
}

func TestAssembleConsoleToken(t *testing.T) {
	a := assembleForTest(t)
	base := "http://" + a.consoleLn.Addr().String()
	for _, tt := range []struct {
		name, token string
		code        int
	}{
		{"正确的令牌", a.token, http.StatusOK},
		{"没有令牌", "", http.StatusUnauthorized},
		{"错误的令牌", "fake-token-0000000000", http.StatusUnauthorized},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, base+"/api/tiers", nil)
			if tt.token != "" {
				req.Header.Set("X-Airlock-Token", tt.token)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != tt.code {
				t.Fatalf("状态码 %d，期望 %d", resp.StatusCode, tt.code)
			}
		})
	}
}

func TestAssembleTerminalOrigin(t *testing.T) {
	a := assembleForTest(t)
	// 网页终端只接受自己的源。控制台的源不同，握手被拒绝。
	req, _ := http.NewRequest(http.MethodGet, "http://"+a.termLn.Addr().String()+"/ws?cabin=x", nil)
	req.Header.Set("Origin", "http://"+a.consoleLn.Addr().String())
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("状态码 %d，期望 %d", resp.StatusCode, http.StatusForbidden)
	}
}
