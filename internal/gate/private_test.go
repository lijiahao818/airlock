package gate

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"slices"
	"testing"
)

// 用户允许了也不能连接本机、局域网和链路本地地址。
func TestConnectRejectsPrivateLiteral(t *testing.T) {
	addrs := echoTarget(t)
	lookups := fakeLookup(t, nil)
	a := &asker{answer: answer(true)}
	g, rec := startGate(t, Config{Ask: a.ask})
	for _, target := range []string{
		"127.0.0.1:443", "127.1.2.3:8080", "[::1]:443",
		"10.1.2.3:443", "172.16.0.1:443", "192.168.65.254:443", "[fd00::1]:443",
		"169.254.169.254:80", "[fe80::1]:443",
		"0.0.0.0:443", "[::]:443", "[::ffff:127.0.0.1]:443",
		// 公网地址以外的特殊用途地址。
		"100.64.0.1:443", "100.100.100.100:443", "198.18.0.1:443", "0.1.2.3:443", "240.0.0.1:443",
		"255.255.255.255:443", "224.0.0.1:443", "192.0.0.8:443", "203.0.113.10:443",
		"[64:ff9b::7f00:1]:443", "[2002:7f00:1::1]:443", "[2001:db8::1]:443", "[ff02::1]:443",
	} {
		if code, _ := connectTo(t, g, target); code != http.StatusForbidden {
			t.Errorf("%s：状态码 %d，want 403", target, code)
		}
		if e := rec.last(t); e.Rule != "private-address" || e.Allowed {
			t.Errorf("%s：事件 %+v", target, e)
		}
	}
	if len(*addrs) != 0 || lookups.Load() != 0 {
		t.Errorf("连接了 %v，DNS 查询 %d 次", *addrs, lookups.Load())
	}
}

func TestConnectRejectsPrivateName(t *testing.T) {
	addrs := echoTarget(t)
	fakeLookup(t, map[string][]string{
		"fake-local.example": {"127.0.0.1"},
		"fake-mixed.example": {fakeAddr, "192.168.1.10"},
		"fake-pkg.example":   {"10.0.0.5"},
	})
	a := &asker{answer: answer(true)}
	g, rec := startGate(t, Config{Allow: []string{"fake-pkg.example"}, Ask: a.ask})
	for _, target := range []string{"fake-local.example:443", "fake-mixed.example:443", "fake-pkg.example:443", "fake-local.example:443"} {
		if code, _ := connectTo(t, g, target); code != http.StatusForbidden {
			t.Errorf("%s：状态码 %d，want 403", target, code)
		}
	}
	if got := rec.rules(); !slices.Equal(got, []string{"private-address", "private-address", "private-address", "private-address"}) {
		t.Errorf("规则 %v", got)
	}
	if len(*addrs) != 0 {
		t.Errorf("连接了 %v", *addrs)
	}
	// 地址检查不改变用户的决定：第二次请求不再暂扣，但仍然被拒绝。
	if a.n.Load() != 2 {
		t.Errorf("Ask 被调用 %d 次，want 2", a.n.Load())
	}
}

// 被拒绝的主机名不能触发 DNS 查询，否则 agent 可以借闸门外带数据。
func TestDeniedHostIsNotResolved(t *testing.T) {
	echoTarget(t)
	lookups := fakeLookup(t, nil)
	g, _ := startGate(t, Config{})
	connectTo(t, g, "fake-secret-data.example:443")
	connectTo(t, g, "fake-secret-data.example:443")
	if lookups.Load() != 0 {
		t.Fatalf("被拒绝的主机名触发了 %d 次 DNS 查询", lookups.Load())
	}
}

func TestPublicLiteralIsNotResolved(t *testing.T) {
	addrs := echoTarget(t)
	lookups := fakeLookup(t, nil)
	g, _ := startGate(t, Config{Ask: (&asker{answer: answer(true)}).ask})
	for _, target := range []string{fakeAddr + ":443", "[2606:4700:4700::1111]:443"} {
		if code, _ := connectTo(t, g, target); code != http.StatusOK {
			t.Errorf("公网地址 %s：状态码 %d", target, code)
		}
	}
	if lookups.Load() != 0 || !slices.Equal(*addrs, []string{fakeAddr + ":443", "[2606:4700:4700::1111]:443"}) {
		t.Errorf("DNS 查询 %d 次，连接了 %v", lookups.Load(), *addrs)
	}
}

func TestConnectLookupFails(t *testing.T) {
	addrs := echoTarget(t)
	saved := lookup
	t.Cleanup(func() { lookup = saved })
	g, _ := startGate(t, Config{Allow: []string{"fake-pkg.example"}})
	for _, fail := range []func(context.Context, string, string) ([]netip.Addr, error){
		func(context.Context, string, string) ([]netip.Addr, error) { return nil, errors.New("fake-nxdomain") },
		func(context.Context, string, string) ([]netip.Addr, error) { return nil, nil },
	} {
		lookup = fail
		if code, _ := connectTo(t, g, "fake-pkg.example:443"); code != http.StatusBadGateway {
			t.Errorf("解析失败：状态码 %d，want 502", code)
		}
	}
	if len(*addrs) != 0 {
		t.Errorf("解析失败时连接了 %v", *addrs)
	}
}
