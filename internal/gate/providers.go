package gate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// provider 描述一个模型供应商的路由。上游地址不导出，单元测试在包内替换为本地 mock。
type provider struct {
	upstream string // 闸门路由转发到的地址
	header   string // 携带密钥的请求头
	bearer   bool   // 请求头的值是否带 "Bearer " 前缀
	testURL  string // TestKey 调用的地址
	testAuth func(r *http.Request, secret string)
}

var providers = map[string]*provider{
	"anthropic": {
		upstream: "https://api.anthropic.com",
		header:   "X-Api-Key",
		testURL:  "https://api.anthropic.com/v1/models",
		testAuth: func(r *http.Request, s string) {
			r.Header.Set("X-Api-Key", s)
			r.Header.Set("Anthropic-Version", "2023-06-01")
		},
	},
	"openai": {
		upstream: "https://api.openai.com",
		header:   "Authorization",
		bearer:   true,
		testURL:  "https://api.openai.com/v1/models",
		testAuth: func(r *http.Request, s string) { r.Header.Set("Authorization", "Bearer "+s) },
	},
	// Anthropic 格式的接口只写明了 /messages，所以测试密钥时用原生的模型列表接口。
	"deepseek": {
		upstream: "https://api.deepseek.com/anthropic",
		header:   "X-Api-Key",
		testURL:  "https://api.deepseek.com/models",
		testAuth: func(r *http.Request, s string) { r.Header.Set("Authorization", "Bearer "+s) },
	},
}

// Providers 返回开放模型路由的供应商。
func Providers() []string { return []string{"anthropic", "openai", "deepseek"} }

// testClient 不跟随重定向。跟随时，X-Api-Key 等自定义请求头会被发往重定向的目标。
var testClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// TestKey 用密钥调用供应商的接口，检查密钥是否可用。
func TestKey(ctx context.Context, name, secret string) error {
	p, ok := providers[name]
	if !ok {
		return errors.New("gate: 未知的模型供应商 " + name)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.testURL, nil)
	if err != nil {
		return err
	}
	p.testAuth(req, secret)
	resp, err := testClient.Do(req)
	if err != nil {
		// 错误中只保留地址，不含请求头。
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("连不上 %s 的接口，请检查网络后重试：%v", name, err)
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode/100 == 2:
		return nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%s 不接受这个密钥，请确认密钥是否复制完整", name)
	}
	return fmt.Errorf("%s 的接口返回了 %d，请稍后重试", name, resp.StatusCode)
}
