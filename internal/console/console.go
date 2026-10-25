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
	// DataDir 是 Airlock 的数据目录。向导是否完成记录在这里，因为控制台每次启动的端口不同，
	// 浏览器的 localStorage 不能跨次保存。
	DataDir string
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
	s.mux.HandleFunc("GET /api/cabins", s.cabinsList)
	s.mux.HandleFunc("POST /api/cabins", s.cabinCreate)
	s.mux.HandleFunc("POST /api/cabins/{id}/start", s.cabinStart)
	s.mux.HandleFunc("POST /api/cabins/{id}/stop", s.cabinStop)
	s.mux.HandleFunc("POST /api/cabins/{id}/reset", s.cabinReset)
	s.mux.HandleFunc("GET /api/cabins/{id}/agent-url", s.agentURL)
	s.mux.HandleFunc("POST /api/cabins/{id}/terminal", s.openTerminal)
	s.mux.HandleFunc("GET /api/cabins/{id}/entry", s.entryFindings)
	s.mux.HandleFunc("GET /api/cabins/{id}/changes", s.reviewItems)
	s.mux.HandleFunc("POST /api/cabins/{id}/revert", s.revert)
	s.mux.HandleFunc("POST /api/cabins/{id}/apply", s.apply)
	s.mux.HandleFunc("POST /api/cabins/{id}/finish", s.finishReview)
	s.mux.HandleFunc("GET /api/cabins/{id}/report", s.report)
	s.mux.HandleFunc("GET /api/cabins/{id}/holds", s.cabinHolds)
	s.mux.HandleFunc("POST /api/holds/{id}", s.decide)
	s.mux.HandleFunc("GET /api/wizard", s.wizardGet)
	s.mux.HandleFunc("POST /api/wizard", s.wizardDone)
	s.mux.HandleFunc("POST /api/cabins/{id}/reset-decision", s.resetDecision)
	s.mux.HandleFunc("GET /api/events", s.events)
	// 兜底：未知的 /api 路径返回中文提示。不写方法，覆盖全部方法。
	s.mux.HandleFunc("/api/", apiNotFound)
}

// server 保存控制台的全部依赖、令牌和路由。
type server struct {
	deps  Deps
	token string
	mux   *http.ServeMux
}
