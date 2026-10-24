package staging

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// roots 打开项目和暂存区的根目录。每次操作结束时关闭，否则 Windows 上无法删除这些目录。
func (a *Area) roots() (proj, files *os.Root, err error) {
	if proj, err = os.OpenRoot(a.project); err != nil {
		return nil, nil, err
	}
	if files, err = os.OpenRoot(a.Files()); err != nil {
		proj.Close()
		return nil, nil, err
	}
	return proj, files, nil
}

// Changes 与基线清单比较，列出暂存区中的改动，按路径排序。
func (a *Area) Changes() ([]Change, error) {
	proj, files, err := a.roots()
	if err != nil {
		return nil, err
	}
	defer proj.Close()
	defer files.Close()

	seen := map[string]bool{}
	err = fs.WalkDir(files.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == "." {
			return err
		}
		// 目录本身不是改动。符号链接不会被 WalkDir 进入，作为特殊文件列出。
		if !d.IsDir() {
			seen[p] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for p := range a.base {
		seen[p] = true
	}
	changes := []Change{}
	for p := range seen {
		c, err := a.classify(proj, files, p)
		if err != nil {
			return nil, err
		}
		if c != nil {
			changes = append(changes, *c)
		}
	}
	slices.SortFunc(changes, func(x, y Change) int { return strings.Compare(x.Path, y.Path) })
	return changes, nil
}

// state 是一个路径在某个根目录中的状态。
type state struct {
	exists  bool
	regular bool
	sum     string // 只有普通文件有
}

// stat 读取 rel 的状态，不跟随最后一级的符号链接。目录、越出根目录等错误都按"存在但不是普通文件"处理。
func stat(root *os.Root, rel string) (state, error) {
	name := filepath.FromSlash(rel)
	info, err := root.Lstat(name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return state{}, nil
	case err != nil, !info.Mode().IsRegular():
		return state{exists: true}, nil
	}
	sum, err := hashFile(root, name)
	if err != nil {
		return state{}, err
	}
	return state{exists: true, regular: true, sum: sum}, nil
}

// classify 给出 rel 的改动。没有改动时返回 nil。
func (a *Area) classify(proj, files *os.Root, rel string) (*Change, error) {
	base, inBase := a.base[rel]
	s, err := stat(files, rel)
	if err != nil {
		return nil, err
	}
	c := &Change{Path: rel}
	switch {
	case !s.exists && !inBase:
		return nil, nil
	case !s.exists:
		c.Kind = Deleted
	case s.regular && s.sum == base:
		return nil, nil
	case inBase:
		c.Kind = Modified
	default:
		c.Kind = Added
	}
	c.Special = s.exists && !s.regular

	p, err := stat(proj, rel)
	if err != nil {
		return nil, err
	}
	// 项目中的文件和暂存区相同时，不算冲突，例如这个改动已经写回过项目。
	same := p.exists == s.exists && (!p.exists || p.regular && s.regular && p.sum == s.sum)
	if inBase {
		c.Conflict = !(p.regular && p.sum == base) && !same
	} else {
		c.Conflict = p.exists && !same
	}
	return c, nil
}
