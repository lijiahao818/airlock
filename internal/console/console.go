package console

import (
	"context"
	"net/http"

	"github.com/lijiahao818/airlock/internal/advisor"
	"github.com/lijiahao818/airlock/internal/cabin"
)

type Keys interface {
	Hint(provider string) (string, error)
	Set(provider, secret string) error
	Delete(provider string) error
	Test(ctx context.Context, provider string) error
}

type AdvisorSettings interface {
	Settings() advisor.Settings
	SetSettings(s advisor.Settings) error
}

type Deps struct {
	Cabins     cabin.Service
	Keys       Keys // cmd/airlock 用 vault 和 gate.TestKey 实现
	Advisor    AdvisorSettings
	PickFolder func(ctx context.Context) (string, error)
}

// New 返回控制台的 HTTP 处理器。token 是启动时生成的随机令牌。
func New(d Deps, token string) http.Handler {
	s := &server{deps: d, token: token, mux: http.NewServeMux()}
	s.routes()
	return s
}

// routes 注册全部路由。
func (s *server) routes() {
	s.mux.Handle("GET /", http.FileServerFS(dist))
}

// server 保存控制台的全部依赖、令牌和路由。
type server struct {
	deps  Deps
	token string
	mux   *http.ServeMux
}
