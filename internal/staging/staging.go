package staging

import "errors"

var errNotImplemented = errors.New("staging: 未实现")

type Kind string

const (
	Added    Kind = "added"
	Modified Kind = "modified"
	Deleted  Kind = "deleted"
)

type Change struct {
	Path     string `json:"path"` // 斜杠分隔的相对路径
	Kind     Kind   `json:"kind"`
	Conflict bool   `json:"conflict"` // 运行期间，项目文件夹中的同一文件也有改动
	Special  bool   `json:"special"`  // 符号链接或特殊文件，不能写回项目
}

type Area struct{}

// Create 在 dir 下建立暂存区，把 project 复制进去。skip 返回 true 的文件不复制。
func Create(project, dir string, skip func(rel string) bool) (*Area, error) {
	return nil, errNotImplemented
}

// Open 打开已有的暂存区。
func Open(project, dir string) (*Area, error) { return nil, errNotImplemented }

// Files 返回挂载进沙箱的目录。
func (a *Area) Files() string { return "" }

func (a *Area) Changes() ([]Change, error) { return nil, errNotImplemented }

// Revert 撤销一个改动，恢复到基线。不在基线清单中的文件从暂存区删除。
func (a *Area) Revert(rel string) error { return errNotImplemented }

// Apply 把一个改动写回项目。
func (a *Area) Apply(rel string) error { return errNotImplemented }

// Discard 删除整个暂存区。
func (a *Area) Discard() error { return errNotImplemented }
