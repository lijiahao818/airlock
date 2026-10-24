package main

import (
	"context"

	"github.com/lijiahao818/airlock/internal/gate"
)

// advisorProviders 是安全副驾可以使用的供应商，见 docs/architecture.md 第 5.8 节。
var advisorProviders = []string{"anthropic", "deepseek"}

// startAdvisorGate 启动安全副驾专用的闸门实例。
// Allow 为空，Ask 始终返回 false，所以这个实例拒绝全部 CONNECT，只转发模型路由。
func startAdvisorGate(secret func(provider string) (string, error)) (*gate.Gate, error) {
	return gate.Start(gate.Config{
		Providers: advisorProviders,
		Secret:    secret,
		Ask:       func(context.Context, gate.Event) bool { return false },
	})
}
