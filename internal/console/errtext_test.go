package console

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// 控制台显示的错误信息不带 Go 的包名前缀（#79）。
func TestUserText(t *testing.T) {
	for in, want := range map[string]string{
		"cabin: 项目文件夹必须是完整路径":            "项目文件夹必须是完整路径",
		"cabin: 复制项目文件失败：staging: 路径不合法": "复制项目文件失败：路径不合法",
		"gate: 未知的模型供应商 x":               "未知的模型供应商 x",
		"filecheck: 读不到文件":               "读不到文件",
		"没有前缀的错误":                        "没有前缀的错误",
	} {
		if got := userText(errors.New(in)); got != want {
			t.Errorf("userText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestErrorsHaveNoPackagePrefix(t *testing.T) {
	e := newEnv(t)
	e.svc.applyErr = errors.New("staging: 项目中的这个文件和开始运行时不一样了")
	e.svc.checkDockerErr = errors.New("sandbox: 找不到 docker 命令")
	for target, method := range map[string]string{"/api/cabins/c1/apply": http.MethodPost, "/api/docker": http.MethodGet} {
		resp := e.request(t, method, target, `{"path":"main.go"}`, fakeToken, "", "")
		got := body(t, resp)
		if resp.StatusCode == http.StatusOK || strings.Contains(got, "staging:") || strings.Contains(got, "sandbox:") {
			t.Errorf("%s：状态码 %d，响应 %s", target, resp.StatusCode, got)
		}
	}
}
