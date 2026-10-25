package staging

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// checkRel 拒绝空路径、根目录本身、不规范的路径、绝对路径和含 .. 的路径。
func checkRel(rel string) error {
	if rel == "" || rel == "." || path.Clean(rel) != rel || strings.ContainsAny(rel, `\:`) || !filepath.IsLocal(filepath.FromSlash(rel)) {
		return errBadPath
	}
	return nil
}

// Apply 把一个改动写回项目。
func (a *Area) Apply(rel string) error {
	if err := checkRel(rel); err != nil {
		return err
	}
	if badName(rel) {
		return errBadName
	}
	proj, files, err := a.roots()
	if err != nil {
		return err
	}
	defer proj.Close()
	defer files.Close()
	c, err := a.classify(proj, files, rel)
	switch {
	case err != nil:
		return err
	case c == nil:
		return errNoChange
	// 已经写回的改动再次写回时什么也不做，所以写回失败后可以重试整个审查。
	case c.Applied:
		return nil
	case c.Conflict:
		return errConflict
	case c.Special:
		return errSpecial
	}
	name := filepath.FromSlash(rel)
	if c.Kind == Deleted {
		if err := proj.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	} else if err := replace(files, proj, name); err != nil {
		return err
	}
	return a.markApplied(rel)
}

// markApplied 记下 rel 已经写回。记录保存在磁盘上，所以 Airlock 重启后仍然知道哪些改动写回过。
// 先写临时文件再改名，写到一半退出时原来的记录保持完整，Open 不会失败。
func (a *Area) markApplied(rel string) error {
	a.applied[rel] = true
	data, _ := json.Marshal(a.applied) // 只有字符串和布尔值，编码不会失败
	tmp := a.appliedFile() + ".tmp"
	err := os.WriteFile(tmp, data, 0o600)
	if err == nil {
		err = os.Rename(tmp, a.appliedFile())
	}
	if err != nil {
		return fmt.Errorf("staging: 文件已经写回项目，但记录失败：%w", err)
	}
	return nil
}
