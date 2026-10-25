package console

import (
	"context"
	"net/http"
	"time"

	"github.com/lijiahao818/airlock/internal/audit"
	"github.com/lijiahao818/airlock/internal/cabin"
)

// CreateReq 是 post /api/cabins 的请求体。前端类型由 tsgen 生成。
type CreateReq struct {
	Project string `json:"project"`
	Preset  string `json:"preset"`
	Tier    string `json:"tier"`
}

// PathReq 是改动类接口的请求体。
type PathReq struct {
	Path string `json:"path"`
}

// HostReq 是 reset-decision 的请求体。
type HostReq struct {
	Host string `json:"host"`
}

// AllowReq 是拦截决定的请求体。
type AllowReq struct {
	Allow bool `json:"allow"`
}

func (s *server) cabinsList(w http.ResponseWriter, r *http.Request) {
	list, err := s.deps.Cabins.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if list == nil {
		list = []cabin.Cabin{}
	}
	writeJSON(w, list)
}

func (s *server) cabinCreate(w http.ResponseWriter, r *http.Request) {
	var req CreateReq
	if !decodeJSON(w, r, &req) {
		return
	}
	c, err := s.deps.Cabins.Create(req.Project, req.Preset, req.Tier)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, c)
}

// opTimeout 是启动、停止和重置的超时。第一次启动要构建 agent 镜像，可能要几分钟。测试中替换它。
var opTimeout = 20 * time.Minute

// opContext 让操作在浏览器刷新或关闭页面后继续运行到结束，否则镜像构建会被中断，工作舱回到未运行。
func opContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(r.Context()), opTimeout)
}

func (s *server) cabinStart(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := opContext(r)
	defer cancel()
	done(w, s.deps.Cabins.Start(ctx, r.PathValue("id")))
}

func (s *server) cabinStop(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := opContext(r)
	defer cancel()
	done(w, s.deps.Cabins.Stop(ctx, r.PathValue("id")))
}

func (s *server) cabinReset(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := opContext(r)
	defer cancel()
	done(w, s.deps.Cabins.Reset(ctx, r.PathValue("id")))
}

// URLResp 是 get /api/cabins/{id}/agent-url 的响应。
type URLResp struct {
	URL string `json:"url"`
}

func (s *server) agentURL(w http.ResponseWriter, r *http.Request) {
	u, err := s.deps.Cabins.AgentURL(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, URLResp{URL: u})
}

func (s *server) openTerminal(w http.ResponseWriter, r *http.Request) {
	done(w, s.deps.Cabins.OpenTerminal(r.PathValue("id")))
}

func (s *server) entryFindings(w http.ResponseWriter, r *http.Request) {
	list, err := s.deps.Cabins.EntryFindings(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if list == nil {
		list = []cabin.Finding{}
	}
	writeJSON(w, list)
}

func (s *server) reviewItems(w http.ResponseWriter, r *http.Request) {
	list, err := s.deps.Cabins.ReviewItems(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if list == nil {
		list = []cabin.ReviewItem{}
	}
	// 单个改动的发现列表也要输出 []，不输出 null。
	for i := range list {
		if list[i].Findings == nil {
			list[i].Findings = []cabin.Finding{}
		}
	}
	writeJSON(w, list)
}

func (s *server) revert(w http.ResponseWriter, r *http.Request) {
	var req PathReq
	if !decodeJSON(w, r, &req) {
		return
	}
	done(w, s.deps.Cabins.Revert(r.PathValue("id"), req.Path))
}

func (s *server) apply(w http.ResponseWriter, r *http.Request) {
	var req PathReq
	if !decodeJSON(w, r, &req) {
		return
	}
	done(w, s.deps.Cabins.Apply(r.PathValue("id"), req.Path))
}

func (s *server) finishReview(w http.ResponseWriter, r *http.Request) {
	done(w, s.deps.Cabins.FinishReview(r.PathValue("id")))
}

func (s *server) report(w http.ResponseWriter, r *http.Request) {
	rep, err := s.deps.Cabins.Report(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if rep.Hosts == nil {
		rep.Hosts = []audit.HostSummary{}
	}
	if rep.Holds == nil {
		rep.Holds = []audit.HoldSummary{}
	}
	if rep.KeyUses == nil {
		rep.KeyUses = map[string]int{}
	}
	if rep.Files == nil {
		rep.Files = []audit.Review{}
	}
	writeJSON(w, rep)
}

func (s *server) decide(w http.ResponseWriter, r *http.Request) {
	var req AllowReq
	if !decodeJSON(w, r, &req) {
		return
	}
	done(w, s.deps.Cabins.Decide(r.PathValue("id"), req.Allow))
}

func (s *server) resetDecision(w http.ResponseWriter, r *http.Request) {
	var req HostReq
	if !decodeJSON(w, r, &req) {
		return
	}
	done(w, s.deps.Cabins.ResetDecision(r.PathValue("id"), req.Host))
}
