package staging

import (
	"errors"
	"io/fs"
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
		return nil
	}
	return replace(files, proj, name)
}
