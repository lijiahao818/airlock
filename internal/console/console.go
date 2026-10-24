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

// routes 注册 /api 路由。静态页面由 ServeHTTP 直接分发，不走 mux。
func (s *server) routes() {
	s.mux.HandleFunc("GET /api/docker", s.docker)
	s.mux.HandleFunc("GET /api/providers", s.providers)
	s.mux.HandleFunc("PUT /api/keys/{provider}", s.setKey)
	s.mux.HandleFunc("DELETE /api/keys/{provider}", s.deleteKey)
	s.mux.HandleFunc("POST /api/keys/{provider}/test", s.testKey)
	s.mux.HandleFunc("GET /api/presets", s.presetsList)
	s.mux.HandleFunc("GET /api/tiers", s.tiers)
	s.mux.HandleFunc("GET /api/advisor", s.advisorGet)
	s.mux.HandleFunc("PUT /api/advisor", s.advisorPut)
	s.mux.HandleFunc("POST /api/pick-folder", s.pickFolder)
	// 兜底：未知的 /api 路径返回中文提示。
	s.mux.HandleFunc("GET /api/", apiNotFound)
	s.mux.HandleFunc("PUT /api/", apiNotFound)
	s.mux.HandleFunc("POST /api/", apiNotFound)
	s.mux.HandleFunc("DELETE /api/", apiNotFound)
}

// server 保存控制台的全部依赖、令牌和路由。
type server struct {
	deps  Deps
	token string
	mux   *http.ServeMux
}
