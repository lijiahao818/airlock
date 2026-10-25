package staging

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Op 是差异中一行的类型。
type Op string

const (
	OpAdd  Op = "add"  // agent 的版本新增的行
	OpDel  Op = "del"  // agent 的版本删除的行
	OpSame Op = "same" // 不变的行
	OpSkip Op = "skip" // 省略的连续不变行
)

type Line struct {
	Op   Op     `json:"op"`
	Text string `json:"text"`           // 不含换行符。Op 为 skip 时为空
	Skip int    `json:"skip,omitempty"` // Op 为 skip 时省略的行数
}

// Diff 是一个改动的内容差异。内容来自沙箱，属于不可信数据，只能当纯文本显示（红线 6）。
type Diff struct {
	Lines  []Line `json:"lines"`
	Reason string `json:"reason,omitempty"` // 不显示内容的原因，给用户看的大白话。不为空时 Lines 为空
}

const (
	// maxDiffSize 是显示内容的文件大小上限，超过的不读取。
	maxDiffSize = 1 << 20
	// diffContext 是每处改动前后保留的不变行数。
	diffContext = 3
	// maxDiffCells 限制逐行比较的计算量。超过时旧内容全部列为删除，新内容全部列为新增。
	maxDiffCells = 4 << 20
)

const (
	reasonSpecial = "这是符号链接、特殊文件，或者 Windows 不能正确读写的文件名。Airlock 不读取它的内容。"
	reasonBinary  = "这是二进制文件，或者不是 UTF-8 编码的文本，所以不显示内容。"
	reasonTooBig  = "文件超过 1 MB，所以不显示内容。"
)

// Diff 比较项目中现在的内容和暂存区中 agent 的版本。写回后，项目中的文件就是 agent 的版本。
// 没有冲突时，项目中现在的内容就是运行开始时的基线。
func (a *Area) Diff(rel string) (Diff, error) {
	if err := checkRel(rel); err != nil {
		return Diff{}, err
	}
	proj, files, err := a.roots()
	if err != nil {
		return Diff{}, err
	}
	defer proj.Close()
	defer files.Close()
	c, err := a.classify(proj, files, rel, nil)
	switch {
	case err != nil:
		return Diff{}, err
	case c == nil:
		return Diff{}, errNoChange
	case c.Special:
		return Diff{Lines: []Line{}, Reason: reasonSpecial}, nil
	}
	old, why, err := readText(proj, rel)
	if err != nil || why != "" {
		return Diff{Lines: []Line{}, Reason: why}, err
	}
	var cur string
	if c.Kind != Deleted {
		if cur, why, err = readText(files, rel); err != nil || why != "" {
			return Diff{Lines: []Line{}, Reason: why}, err
		}
	}
	return Diff{Lines: diffLines(old, cur)}, nil
}

// readText 读取 root 中 rel 的文本。文件不存在时返回空文本。
// 不是普通文件、越出 root、超过上限、是二进制或不是 UTF-8 时，返回不显示内容的原因。
func readText(root *os.Root, rel string) (text, reason string, err error) {
	f, info, err := openRegular(root, filepath.FromSlash(rel))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", "", nil
	case err != nil:
		return "", reasonSpecial, nil
	}
	defer f.Close()
	if info.Size() > maxDiffSize {
		return "", reasonTooBig, nil
	}
	// 文件可能在读取期间变大，所以读取时也限制大小。
	b, err := io.ReadAll(io.LimitReader(f, maxDiffSize+1))
	switch {
	case err != nil:
		return "", "", err
	case len(b) > maxDiffSize:
		return "", reasonTooBig, nil
	case bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b):
		return "", reasonBinary, nil
	}
	return string(b), "", nil
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// diffLines 逐行比较 old 和 cur，只保留改动和前后 diffContext 行。
func diffLines(old, cur string) []Line {
	a, b := splitLines(old), splitLines(cur)
	// 相同的开头和结尾不参与比较，减少计算量。
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	var lines []Line
	for _, s := range a[:pre] {
		lines = append(lines, Line{Op: OpSame, Text: s})
	}
	lines = append(lines, lcs(a[pre:len(a)-suf], b[pre:len(b)-suf])...)
	for _, s := range a[len(a)-suf:] {
		lines = append(lines, Line{Op: OpSame, Text: s})
	}
	return collapse(lines)
}

// lcs 用最长公共子序列比较 a 和 b。
func lcs(a, b []string) []Line {
	n, m := len(a), len(b)
	var lines []Line
	if n*m > maxDiffCells {
		for _, s := range a {
			lines = append(lines, Line{Op: OpDel, Text: s})
		}
		for _, s := range b {
			lines = append(lines, Line{Op: OpAdd, Text: s})
		}
		return lines
	}
	// t[i*(m+1)+j] 是 a[i:] 和 b[j:] 的最长公共子序列的长度。
	t := make([]int32, (n+1)*(m+1))
	at := func(i, j int) int32 { return t[i*(m+1)+j] }
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				t[i*(m+1)+j] = at(i+1, j+1) + 1
			} else {
				t[i*(m+1)+j] = max(at(i+1, j), at(i, j+1))
			}
		}
	}
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			lines = append(lines, Line{Op: OpSame, Text: a[i]})
			i, j = i+1, j+1
		case at(i+1, j) >= at(i, j+1):
			lines = append(lines, Line{Op: OpDel, Text: a[i]})
			i++
		default:
			lines = append(lines, Line{Op: OpAdd, Text: b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		lines = append(lines, Line{Op: OpDel, Text: a[i]})
	}
	for ; j < m; j++ {
		lines = append(lines, Line{Op: OpAdd, Text: b[j]})
	}
	return lines
}

// collapse 把离改动超过 diffContext 行的连续不变行合并成一个 skip 行。
func collapse(lines []Line) []Line {
	keep := make([]bool, len(lines))
	for i, l := range lines {
		if l.Op == OpSame {
			continue
		}
		for k := max(0, i-diffContext); k <= min(len(lines)-1, i+diffContext); k++ {
			keep[k] = true
		}
	}
	out := []Line{}
	for i := 0; i < len(lines); {
		if keep[i] {
			out = append(out, lines[i])
			i++
			continue
		}
		j := i
		for j < len(lines) && !keep[j] {
			j++
		}
		out = append(out, Line{Op: OpSkip, Skip: j - i})
		i = j
	}
	return out
}
