package staging

import "path/filepath"

// Revert 撤销一个改动，恢复到基线。不在基线清单中的文件从暂存区删除。
// 它只从项目复制基线清单中的文件，所以进舱检查排除的文件不会进入暂存区。
func (a *Area) Revert(rel string) error {
	if err := checkRel(rel); err != nil {
		return err
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
	if c == nil {
		return errNoChange
	}
	base, inBase := a.base[rel]
	name := filepath.FromSlash(rel)
	// 新增的文件只从暂存区删除，不读取项目，所以项目中有同名文件也可以撤销。
	// 例如 agent 新建的 .env：项目中的 .env 被进舱检查排除，这个改动总是冲突。
	if !inBase {
		return files.RemoveAll(name)
	}
	// 项目中的文件必须仍是基线，否则复制回去的不是运行开始时的内容。冲突的改动都在这里被拒绝。
	if p, err := stat(proj, rel); err != nil || !p.regular || p.sum != base {
		return errConflict
	}
	if err := files.RemoveAll(name); err != nil {
		return err
	}
	_, err = copyFile(proj, files, rel)
	return err
}
