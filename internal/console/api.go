package console

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"

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

// pkgPrefix 去掉错误信息中的包名前缀，例如 "cabin: "。用户看到的应该是中文大白话（CLAUDE.md 第 15 节）。
// 日志和测试中的错误信息不受影响。
var pkgPrefix = strings.NewReplacer(
	"cabin: ", "", "staging: ", "", "gate: ", "", "filecheck: ", "", "advisor: ", "",
	"sandbox: ", "", "vault: ", "", "audit: ", "", "presets: ", "",
)

// userText 返回给用户看的错误信息。
func userText(err error) string { return pkgPrefix.Replace(err.Error()) }

// fail 把错误返回给用户。
func fail(w http.ResponseWriter, err error) {
	http.Error(w, userText(err), http.StatusBadRequest)
}

// done 输出操作类接口的响应：err 为 nil 时输出 {}，否则把错误返回给用户。
func done(w http.ResponseWriter, err error) {
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, struct{}{})
}

// apiNotFound 拒绝未知的 /api 路径。
func apiNotFound(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Airlock 没有这个接口。请更新 Airlock 后重试。", http.StatusNotFound)
}

func (s *server) docker(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.Cabins.CheckDocker(r.Context()); err != nil {
		http.Error(w, "Docker 没有就绪。"+userText(err), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, struct{}{})
}

// ProviderInfo 是 get /api/providers 列表中的一项。前端类型由 tsgen 生成。
type ProviderInfo struct {
	Name string `json:"name"`
	Hint string `json:"hint"` // 密钥的后 4 位。没有保存密钥时为空。
}

func (s *server) providers(w http.ResponseWriter, r *http.Request) {
	list := []ProviderInfo{}
	for _, name := range gate.Providers() {
		hint, _ := s.deps.Keys.Hint(name) // 没有密钥时显示为空
		list = append(list, ProviderInfo{Name: name, Hint: hint})
	}
	writeJSON(w, list)
}

// SecretReq 是 put /api/keys/{provider} 的请求体。
type SecretReq struct {
	Secret string `json:"secret"`
}

func (s *server) setKey(w http.ResponseWriter, r *http.Request) {
	var req SecretReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Secret == "" {
		http.Error(w, "密钥是空的。请粘贴完整的密钥后再保存。", http.StatusBadRequest)
		return
	}
	if err := s.deps.Keys.Set(r.PathValue("provider"), req.Secret); err != nil {
		// 下层的错误一般不含密钥，但万一含了，不能原样返回（红线：密钥不落地）。
		if strings.Contains(err.Error(), req.Secret) {
			http.Error(w, "保存密钥失败，错误的详情里带了密钥，已隐藏。请检查后重试。", http.StatusBadRequest)
		} else {
			fail(w, err)
		}
		return
	}
	writeJSON(w, struct{}{})
}

func (s *server) deleteKey(w http.ResponseWriter, r *http.Request) {
	done(w, s.deps.Keys.Delete(r.PathValue("provider")))
}

func (s *server) testKey(w http.ResponseWriter, r *http.Request) {
	done(w, s.deps.Keys.Test(r.Context(), r.PathValue("provider")))
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
	done(w, s.deps.Advisor.SetSettings(st))
}

// PathResp 是 post /api/pick-folder 的响应。
type PathResp struct {
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
	writeJSON(w, PathResp{Path: p})
}

// LangReq 是 post /api/project-languages 的请求体。前端类型由 tsgen 生成。
type LangReq struct {
	Path string `json:"path"`
}

// LangResp 是 post /api/project-languages 的响应。
type LangResp struct {
	Languages []string `json:"languages"`
}

// projectLanguages 检测项目文件夹使用的语言，用来在新建工作舱时提示语言环境不匹配。
func (s *server) projectLanguages(w http.ResponseWriter, r *http.Request) {
	var req LangReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if !filepath.IsAbs(req.Path) {
		http.Error(w, "项目文件夹必须是完整路径。请重新选择文件夹。", http.StatusBadRequest)
		return
	}
	langs, err := detectLanguages(r.Context(), filepath.Clean(req.Path))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, LangResp{Languages: langs})
}
