package presets

import "errors"

var errNotImplemented = errors.New("presets: 未实现")

// Preset 对应一个 agent 目录中的 preset.json。值中可以使用的模板变量见
// docs/architecture.md 第 5.6 节。
type Preset struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Provider   string            `json:"provider"`        // gate.Providers() 中的一个
	UI         string            `json:"ui"`              // terminal 或 web
	Cmd        []string          `json:"cmd"`             // terminal：在终端中运行的命令。web：容器的主进程
	Env        map[string]string `json:"env,omitempty"`   // 值中可以使用模板变量
	Files      map[string]string `json:"files,omitempty"` // 沙箱内的路径 → 内容，可以使用模板变量
	Web        *Web              `json:"web,omitempty"`   // UI 为 web 时必填
	Dockerfile []byte            `json:"-"`               // 来自同目录的 Dockerfile
}

type Web struct {
	Port         int    `json:"port"`         // 沙箱内网页服务的端口
	TokenPattern string `json:"tokenPattern"` // 从启动输出中提取令牌的正则表达式，必须有一个分组
	OpenPath     string `json:"openPath"`     // 网页界面的路径，例如 /?token={token}
}

func All() ([]Preset, error) { return nil, errNotImplemented }

func Get(id string) (Preset, error) { return Preset{}, errNotImplemented }
