package main

import (
	"bufio"
	"net"
	"net/http"
	"strconv"
	"testing"

	"github.com/lijiahao818/airlock/internal/vault"
)

func fakeSecret(provider string) (string, error) {
	if provider == "anthropic" {
		return "fake-anthropic-key", nil
	}
	return "", vault.ErrNotFound
}

func TestAdvisorGateRoutes(t *testing.T) {
	g, err := startAdvisorGate(fakeSecret)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	// 占位符不对时，开放的路由返回 403（bad-placeholder），没有开放的路由返回 404（unsupported）。请求都不离开本机。
	want := map[string]int{"anthropic": http.StatusForbidden, "deepseek": http.StatusForbidden, "openai": http.StatusNotFound}
	for p, code := range want {
		req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:"+strconv.Itoa(g.Port())+"/"+p+"/v1/messages", nil)
		req.Header.Set("X-Api-Key", "fake-placeholder")
		req.Header.Set("Authorization", "Bearer fake-placeholder")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != code {
			t.Errorf("%s 路由返回 %d，期望 %d", p, resp.StatusCode, code)
		}
	}
}

func TestAdvisorGateDeniesConnect(t *testing.T) {
	g, err := startAdvisorGate(fakeSecret)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	// 同一个主机连续请求两次：第一次经过暂扣，第二次命中本次运行的拒绝记录。两次都必须拒绝。
	for i := range 2 {
		c, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(g.Port()))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Write([]byte("CONNECT api.anthropic.com:443 HTTP/1.1\r\nHost: api.anthropic.com:443\r\n\r\n")); err != nil {
			t.Fatal(err)
		}
		resp, err := http.ReadResponse(bufio.NewReader(c), nil)
		c.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode == http.StatusOK {
			t.Fatalf("第 %d 次 CONNECT 被放行", i+1)
		}
	}
}
