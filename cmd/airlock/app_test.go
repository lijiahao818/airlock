package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
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

// 组装中途失败时返回错误，已经打开的端口和闸门随之关闭。
func TestAssembleFails(t *testing.T) {
	t.Run("数据目录是文件", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "data")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := assemble(file, fakeBackend{}, fakeSecret); err == nil {
			t.Fatal("期望报错，得到 nil")
		}
	})
	t.Run("工作舱列表损坏", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "cabins.json"), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := assemble(dir, fakeBackend{}, fakeSecret); err == nil {
			t.Fatal("期望报错，得到 nil")
		}
	})
}

// keys 把调用转给 vault。Windows 上 vault 会访问真实的凭据管理器，所以只在其他系统上运行。
// 其他系统不支持钥匙串，每个方法都返回错误，不访问外网。
func TestKeysForwardToVault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上会访问真实的凭据管理器")
	}
	var k keys
	if _, err := k.Hint("anthropic"); err == nil {
		t.Error("Hint：期望报错")
	}
	if err := k.Set("anthropic", "fake-key"); err == nil {
		t.Error("Set：期望报错")
	}
	if err := k.Delete("anthropic"); err == nil {
		t.Error("Delete：期望报错")
	}
	if err := k.Test(t.Context(), "anthropic"); err == nil {
		t.Error("Test：期望报错")
	}
}
