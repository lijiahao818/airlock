package gate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const fakeSecret = "fake-real-secret-1234"

// mockUpstream 把全部供应商的上游地址换成本地 mock，测试结束时恢复。
func mockUpstream(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	saved := map[string]provider{}
	for name, p := range providers {
		saved[name] = *p
		p.upstream = srv.URL + "/" + name + "-upstream"
		p.testURL = srv.URL + "/" + name + "-test"
	}
	t.Cleanup(func() {
		for name, p := range saved {
			*providers[name] = p
		}
	})
	return srv
}

func TestTestKeySendsSecret(t *testing.T) {
	want := map[string][2]string{
		"anthropic": {"X-Api-Key", fakeSecret},
		"openai":    {"Authorization", "Bearer " + fakeSecret},
		"deepseek":  {"Authorization", "Bearer " + fakeSecret},
	}
	got := map[string]string{}
	mockUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), "-test")
		got[name] = r.Header.Get(want[name][0])
	})
	for _, name := range Providers() {
		if err := TestKey(context.Background(), name, fakeSecret); err != nil {
			t.Errorf("TestKey(%s): %v", name, err)
		}
		if got[name] != want[name][1] {
			t.Errorf("%s 收到的 %s = %q, want %q", name, want[name][0], got[name], want[name][1])
		}
	}
}

func TestTestKeyRejected(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError} {
		mockUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) })
		err := TestKey(context.Background(), "anthropic", fakeSecret)
		if err == nil {
			t.Fatalf("状态码 %d 时 TestKey 没有报错", status)
		}
		if strings.Contains(err.Error(), fakeSecret) {
			t.Fatalf("错误信息中有密钥：%v", err)
		}
	}
}

func TestTestKeyUnreachable(t *testing.T) {
	srv := mockUpstream(t, func(w http.ResponseWriter, r *http.Request) {})
	srv.Close()
	err := TestKey(context.Background(), "openai", fakeSecret)
	if err == nil || strings.Contains(err.Error(), fakeSecret) {
		t.Fatalf("TestKey = %v; want 不含密钥的错误", err)
	}
}
