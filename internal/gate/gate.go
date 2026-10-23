package gate

import (
	"context"
	"errors"
	"time"
)

var errNotImplemented = errors.New("gate: 未实现")

// Event 是一次出网判定的元数据。不含请求体、请求头和查询参数。
type Event struct {
	Time     time.Time `json:"time"`
	Host     string    `json:"host"`
	Port     int       `json:"port"`
	Method   string    `json:"method"`         // CONNECT，或模型路由上的 HTTP 方法
	Path     string    `json:"path,omitempty"` // 只有模型路由有路径，不含查询参数
	Rule     string    `json:"rule"`           // 命中的规则，取值见 docs/architecture.md 第 5.1 节
	Allowed  bool      `json:"allowed"`
	Provider string    `json:"provider,omitempty"` // 换上了真实密钥时为供应商名，否则为空
}

type Config struct {
	// Host 是非 CONNECT 请求必须携带的 Host 头。为空时要求 127.0.0.1:<闸门端口>。
	Host      string
	Allow     []string // 自动放行的主机，来自安全档位
	Providers []string // 开放的模型路由
	// Secret 返回供应商的真实密钥。闸门不缓存返回值。
	Secret func(provider string) (string, error)
	// Ask 在请求被暂扣时调用。返回 true 表示用户允许。
	// ctx 在 HoldTimeout 后取消，这时调用方必须返回 false。
	Ask     func(ctx context.Context, e Event) bool
	OnEvent func(e Event)
}

const HoldTimeout = 60 * time.Second

type Gate struct{}

// Start 监听 127.0.0.1 上的随机端口。
func Start(cfg Config) (*Gate, error) { return nil, errNotImplemented }

func (g *Gate) Port() int { return 0 }

// Placeholder 返回本实例为供应商签发的占位符。
func (g *Gate) Placeholder(provider string) string { return "" }

func (g *Gate) Close() error { return errNotImplemented }

// Providers 返回开放模型路由的供应商。
func Providers() []string { return []string{"anthropic", "openai", "deepseek"} }

// Tiers 返回全部安全档位。
func Tiers() []string { return []string{"strict", "standard", "permissive"} }

// TierHosts 返回档位自动放行的主机。
func TierHosts(tier string) ([]string, error) { return nil, errNotImplemented }

// TestKey 用密钥调用供应商的接口，检查密钥是否可用。
func TestKey(ctx context.Context, provider, secret string) error { return errNotImplemented }
