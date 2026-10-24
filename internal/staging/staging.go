package staging

import (
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
	Conflict bool   `json:"conflict"` // 运行期间，项目文件夹中的同一文件也有改动
	Special  bool   `json:"special"`  // 符号链接或特殊文件，不能写回项目
}

var (
	errBadPath  = errors.New("staging: 路径不合法")
	errNoChange = errors.New("staging: 这个文件没有改动")
	errConflict = errors.New("staging: 项目中的这个文件在运行期间也被修改了")
	errSpecial  = errors.New("staging: 符号链接和特殊文件不能写回项目")
)

// Area 是一个暂存区。dir 下的 files 目录挂载进沙箱，manifest.json 是基线清单。
// 暂存区的内容来自沙箱，属于不可信数据。全部文件操作都经过 os.Root，不会越出项目和暂存区。
type Area struct {
	project string
	dir     string
	base    map[string]string // 斜杠分隔的相对路径 → SHA-256
}

const progressBatch = 100

// Create 在 dir 下建立暂存区，把 project 复制进去。skip 返回 true 的文件和目录不复制。
// progress 报告复制进度，可以为 nil。符号链接和特殊文件不复制，也不跟随。
func Create(project, dir string, skip func(rel string) bool, progress func(done, total int)) (*Area, error) {
	a := &Area{project: project, dir: dir, base: map[string]string{}}
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
	return a, nil
}

// Files 返回挂载进沙箱的目录。
func (a *Area) Files() string { return filepath.Join(a.dir, "files") }

func (a *Area) manifest() string { return filepath.Join(a.dir, "manifest.json") }

// Discard 删除整个暂存区。
func (a *Area) Discard() error { return os.RemoveAll(a.dir) }

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
