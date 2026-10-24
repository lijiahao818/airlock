package console

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/lijiahao818/airlock/internal/advisor"
	"github.com/lijiahao818/airlock/internal/gate"
	"github.com/lijiahao818/airlock/presets"
)

// maxBody 是请求体的大小上限。控制台只提交密钥和设置，超过上限的不是正常操作。
const maxBody = 64 << 10

// writeJSON 输出 JSON 响应。
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// decodeJSON 解析请求体。请求体超限或不是 JSON 时写好错误响应并返回 false。
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(v)
	if err == nil {
		return true
	}
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		http.Error(w, "请求的内容太大。请刷新页面后重试。", http.StatusRequestEntityTooLarge)
	} else {
		http.Error(w, "请求的内容不是有效的 JSON。请刷新页面后重试。", http.StatusBadRequest)
	}
	return false
}

// apiNotFound 拒绝未知的 /api 路径。
func apiNotFound(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Airlock 没有这个接口。请更新 Airlock 后重试。", http.StatusNotFound)
}

func (s *server) docker(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.Cabins.CheckDocker(r.Context()); err != nil {
		http.Error(w, "Docker 没有就绪。"+err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, struct{}{})
}

type providerInfo struct {
	Name string `json:"name"`
	Hint string `json:"hint"` // 密钥的后 4 位。没有保存密钥时为空。
}

func (s *server) providers(w http.ResponseWriter, r *http.Request) {
	list := []providerInfo{}
	for _, name := range gate.Providers() {
		hint, _ := s.deps.Keys.Hint(name) // 没有密钥时显示为空
		list = append(list, providerInfo{Name: name, Hint: hint})
	}
	writeJSON(w, list)
}

type secretReq struct {
	Secret string `json:"secret"`
}

func (s *server) setKey(w http.ResponseWriter, r *http.Request) {
	var req secretReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Secret == "" {
		http.Error(w, "密钥是空的。请粘贴完整的密钥后再保存。", http.StatusBadRequest)
		return
	}
	if err := s.deps.Keys.Set(r.PathValue("provider"), req.Secret); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, struct{}{})
}

func (s *server) deleteKey(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.Keys.Delete(r.PathValue("provider")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, struct{}{})
}

func (s *server) testKey(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.Keys.Test(r.Context(), r.PathValue("provider")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, struct{}{})
}

func (s *server) presetsList(w http.ResponseWriter, r *http.Request) {
	all, err := presets.All()
	if err != nil {
		http.Error(w, "预设加载失败。请重新安装 Airlock："+err.Error(), http.StatusInternalServerError)
		return
	}
	if all == nil {
		all = []presets.Preset{}
	}
	writeJSON(w, all)
}

func (s *server) tiers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, gate.Tiers())
}

func (s *server) advisorGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.deps.Advisor.Settings())
}

func (s *server) advisorPut(w http.ResponseWriter, r *http.Request) {
	var st advisor.Settings
	if !decodeJSON(w, r, &st) {
		return
	}
	if err := s.deps.Advisor.SetSettings(st); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, struct{}{})
}

type pathResp struct {
	Path string `json:"path"`
}

func (s *server) pickFolder(w http.ResponseWriter, r *http.Request) {
	if s.deps.PickFolder == nil {
		http.Error(w, "这台电脑不支持选择文件夹。请重新安装 Airlock 后重试。", http.StatusInternalServerError)
		return
	}
	p, err := s.deps.PickFolder(r.Context())
	if err != nil {
		http.Error(w, "选择文件夹失败。请重试："+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, pathResp{Path: p})
}
