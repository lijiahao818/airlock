package console

import (
	"net/http"
	"os"
	"path/filepath"
)

// WizardResp 是 get /api/wizard 的响应。前端类型由 tsgen 生成。
type WizardResp struct {
	Done bool `json:"done"`
}

// wizardFile 是数据目录中表示向导已完成的文件。
const wizardFile = "wizard-done"

// wizardGet 返回向导是否已经完成。已经有工作舱的用户也算完成，这样升级前的用户不会再看到向导。
func (s *server) wizardGet(w http.ResponseWriter, r *http.Request) {
	done := false
	if _, err := os.Stat(filepath.Join(s.deps.DataDir, wizardFile)); err == nil {
		done = true
	} else if list, err := s.deps.Cabins.List(); err == nil && len(list) > 0 {
		done = true
	}
	writeJSON(w, WizardResp{Done: done})
}

// wizardDone 记录向导已完成。
func (s *server) wizardDone(w http.ResponseWriter, r *http.Request) {
	if err := os.WriteFile(filepath.Join(s.deps.DataDir, wizardFile), nil, 0o600); err != nil {
		http.Error(w, "记录向导状态失败："+err.Error()+"。下次打开 Airlock 时会再次显示向导。", http.StatusInternalServerError)
		return
	}
	writeJSON(w, struct{}{})
}
