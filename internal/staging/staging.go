package staging

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

type Kind string

const (
	Added    Kind = "added"
	Modified Kind = "modified"
	Deleted  Kind = "deleted"
)

type Change struct {
	Path     string `json:"path"` // 斜杠分隔的相对路径
	Kind     Kind   `json:"kind"`
	Conflict bool   `json:"conflict"` // 项目文件夹中的同一文件和开始运行时不一样
	Special  bool   `json:"special"`  // 符号链接、特殊文件，或 Windows 不能正确读写的文件名，不能写回项目
	Applied  bool   `json:"applied"`  // applied.json 中记录这个改动已经写回。不是冲突，也不需要再写回
}

var (
	errBadPath  = errors.New("staging: 路径不合法")
	errNoChange = errors.New("staging: 这个文件没有改动")
	errConflict = errors.New("staging: 项目中的这个文件和开始运行时不一样了，不能自动写回。你的版本会保留。要用 agent 的版本，请在项目中手动合并")
	errSpecial  = errors.New("staging: 符号链接和特殊文件不能写回项目")
	errBadName  = errors.New("staging: 文件名以点或空格结尾，Windows 不能正确读写这个文件，所以不能写回项目，也不能撤销")
)

// Area 是一个暂存区。dir 下的 files 目录挂载进沙箱，manifest.json 是基线清单。
// 暂存区的内容来自沙箱，属于不可信数据。全部文件操作都经过 os.Root，不会越出项目和暂存区。
type Area struct {
	project string
	dir     string
	base    map[string]string // 斜杠分隔的相对路径 → SHA-256
	applied map[string]bool   // 已经写回项目的路径，保存在 applied.json
}

const progressBatch = 100

// Create 在 dir 下建立暂存区，把 project 复制进去。skip 返回 true 的文件和目录不复制。
// progress 报告复制进度，可以为 nil。符号链接和特殊文件不复制，也不跟随。
func Create(project, dir string, skip func(rel string) bool, progress func(done, total int)) (*Area, error) {
	a := &Area{project: project, dir: dir, base: map[string]string{}, applied: map[string]bool{}}
	_, statErr := os.Stat(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Mkdir(a.Files(), 0o755); err != nil {
		return nil, err
	}
	err := a.copyProject(skip, progress)
	if err == nil {
		var data []byte
		if data, err = json.Marshal(a.base); err == nil {
			err = os.WriteFile(a.manifest(), data, 0o600)
		}
	}
	if err != nil {
		// 只删除本次创建的内容。dir 原来就存在时，保留它和其中的其他文件。
		os.RemoveAll(a.Files())
		os.Remove(a.manifest())
		if errors.Is(statErr, fs.ErrNotExist) {
			os.Remove(dir)
		}
		return nil, err
	}
	return a, nil
}

func (a *Area) copyProject(skip func(string) bool, progress func(int, int)) error {
	src, err := os.OpenRoot(a.project)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenRoot(a.Files())
	if err != nil {
		return err
	}
	defer dst.Close()

	var dirs, files []string
	err = fs.WalkDir(src.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == "." {
			return err
		}
		if skip != nil && skip(p) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		switch {
		case d.IsDir():
			dirs = append(dirs, p)
		case d.Type().IsRegular():
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, d := range dirs {
		if err := dst.Mkdir(filepath.FromSlash(d), 0o755); err != nil {
			return err
		}
	}
	for i, p := range files {
		sum, err := copyFile(src, dst, p)
		if err != nil {
			return err
		}
		a.base[p] = sum
		if progress != nil && (i+1)%progressBatch == 0 && i+1 < len(files) {
			progress(i+1, len(files))
		}
	}
	if progress != nil {
		progress(len(files), len(files))
	}
	return nil
}

// Open 打开已有的暂存区。
func Open(project, dir string) (*Area, error) {
	a := &Area{project: project, dir: dir}
	data, err := os.ReadFile(a.manifest())
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &a.base); err != nil {
		return nil, err
	}
	if _, err := os.Stat(a.Files()); err != nil {
		return nil, err
	}
	a.applied = map[string]bool{}
	data, err = os.ReadFile(a.appliedFile())
	if errors.Is(err, fs.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &a.applied); err != nil {
		return nil, err
	}
	return a, nil
}

// Files 返回挂载进沙箱的目录。
func (a *Area) Files() string { return filepath.Join(a.dir, "files") }

func (a *Area) manifest() string { return filepath.Join(a.dir, "manifest.json") }

// appliedFile 在 files 目录以外，沙箱不能修改它。
func (a *Area) appliedFile() string { return filepath.Join(a.dir, "applied.json") }

// Discard 删除整个暂存区。
func (a *Area) Discard() error { return os.RemoveAll(a.dir) }

// replace 用 src 中的 name 替换 dst 中的 name。缺少的父目录会被重建，例如 agent 删除了整个目录。
// 先复制到随机名的临时文件，成功后再改名，复制失败时 dst 中原来的内容保持不变。
func replace(src, dst *os.Root, name string) error {
	if dir := filepath.Dir(name); dir != "." {
		if err := dst.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	b := make([]byte, 8)
	rand.Read(b)
	tmp := name + ".airlock-" + hex.EncodeToString(b)
	if _, err := copyFileTo(src, dst, name, tmp); err != nil {
		dst.Remove(tmp)
		return err
	}
	// 改名不能覆盖目录和 junction。原路径不是普通文件时先删除，它不会被跟随。
	if info, err := dst.Lstat(name); err == nil && !info.Mode().IsRegular() {
		if err := dst.RemoveAll(name); err != nil {
			dst.Remove(tmp)
			return err
		}
	}
	if err := dst.Rename(tmp, name); err != nil {
		dst.Remove(tmp)
		return err
	}
	return nil
}

func copyFile(src, dst *os.Root, rel string) (string, error) {
	name := filepath.FromSlash(rel)
	return copyFileTo(src, dst, name, name)
}

// copyFileTo 把 src 中的 from 复制为 dst 中的 to，返回内容的 SHA-256。to 必须不存在。
func copyFileTo(src, dst *os.Root, from, to string) (string, error) {
	in, err := src.Open(from)
	if err != nil {
		return "", err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errSpecial
	}
	out, err := dst.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm()|0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, h), in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return hex.EncodeToString(h.Sum(nil)), err
}

func hashFile(root *os.Root, name string) (string, error) {
	f, err := root.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
