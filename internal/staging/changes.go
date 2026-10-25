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
		if p != "." && badName(p) {
			// Windows 打不开这类目录，所以不进入，把目录本身作为一个改动列出。
			seen[p] = true
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
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

// badName 判断 rel 中是否有以点或空格结尾的路径段。Linux 允许这样的文件名，
// 但 Windows 打开路径时去掉末尾的点和空格，读写的是另一个文件。
func badName(rel string) bool {
	return slices.ContainsFunc(strings.Split(rel, "/"), func(s string) bool {
		return strings.HasSuffix(s, ".") || strings.HasSuffix(s, " ")
	})
}

// classify 给出 rel 的改动。没有改动时返回 nil。
func (a *Area) classify(proj, files *os.Root, rel string) (*Change, error) {
	// 这类文件读不到真实内容，所以不读取，只列出并标为不能写回。基线中不会有这类文件。
	if badName(rel) {
		return &Change{Path: rel, Kind: Added, Special: true}, nil
	}
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
	// 写回后项目中的文件不再是基线，按下面的规则会被判为冲突。
	if a.applied[rel] {
		c.Applied = true
		return c, nil
	}

	p, err := stat(proj, rel)
	if err != nil {
		return nil, err
	}
	if inBase {
		// 改写过的文件与改写前内容的哈希比较。项目中的文件没有变时不算冲突。
		want := base
		if orig, ok := a.rewritten[rel]; ok {
			want = orig
		}
		c.Conflict = !(p.regular && p.sum == want)
	} else {
		c.Conflict = p.exists
	}
	return c, nil
}
