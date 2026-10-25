package staging

import "path/filepath"

// Revert 撤销一个改动，恢复到基线。不在基线清单中的文件从暂存区删除。
// 它只从项目复制基线清单中的文件，所以进舱检查排除的文件不会进入暂存区。
func (a *Area) Revert(rel string) error {
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
	if err != nil {
		return err
	}
	switch {
	case c == nil:
		return errNoChange
	// 冲突时项目中的文件已经不是基线，复制回去的不是运行开始时的内容。
	// 项目已经和暂存区一致时，撤销只改暂存区，项目中仍然保留这个改动。
	case c.Conflict, c.Applied:
		return errConflict
	}
	name := filepath.FromSlash(rel)
	if _, inBase := a.base[rel]; !inBase {
		return files.RemoveAll(name)
	}
	return replace(proj, files, name)
}
