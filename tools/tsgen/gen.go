package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

const header = "// 本文件由 go run ./tools/tsgen 生成，不要手动修改。\n"

// pkgInfo 是一个 Go 包中与生成有关的声明。
type pkgInfo struct {
	name   string                   // Go 包名，也是 TS 文件名
	dir    string                   // 相对于仓库根目录的路径
	order  []string                 // 类型的声明顺序
	types  map[string]*ast.TypeSpec // 类型名 → 声明
	docs   map[string]string        // 类型名 → 文档注释
	consts map[string][]string      // 字符串类型名 → 常量值
}

// generate 读取 root 下的契约代码，返回 TS 文件名 → 内容。
func generate(root string) (map[string]string, error) {
	pkgs := map[string]*pkgInfo{}
	var names []string
	for _, dir := range packages {
		p, err := parsePackage(root, dir)
		if err != nil {
			return nil, err
		}
		pkgs[p.name] = p
		names = append(names, p.name)
	}
	out := map[string]string{}
	for _, n := range names {
		s, err := render(pkgs[n], pkgs)
		if err != nil {
			return nil, fmt.Errorf("%s：%w", pkgs[n].dir, err)
		}
		out[n+".ts"] = s
	}
	return out, nil
}

func parsePackage(root, dir string) (*pkgInfo, error) {
	matches, err := filepath.Glob(filepath.Join(root, dir, "*.go"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	p := &pkgInfo{dir: dir, types: map[string]*ast.TypeSpec{}, docs: map[string]string{}, consts: map[string][]string{}}
	fset := token.NewFileSet()
	for _, m := range matches {
		if strings.HasSuffix(m, "_test.go") {
			continue
		}
		src, err := os.ReadFile(m)
		if err != nil {
			return nil, err
		}
		f, err := parser.ParseFile(fset, m, src, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		p.name = f.Name.Name
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range g.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					p.order = append(p.order, s.Name.Name)
					p.types[s.Name.Name] = s
					doc := s.Doc
					if doc == nil && len(g.Specs) == 1 {
						doc = g.Doc
					}
					p.docs[s.Name.Name] = doc.Text()
				case *ast.ValueSpec:
					addConsts(p, g.Tok, s)
				}
			}
		}
	}
	if p.name == "" {
		return nil, fmt.Errorf("%s 中没有 Go 文件", dir)
	}
	return p, nil
}

// addConsts 记录 `Name Type = "value"` 形式的字符串常量。
func addConsts(p *pkgInfo, tok token.Token, s *ast.ValueSpec) {
	typ, ok := s.Type.(*ast.Ident)
	if tok != token.CONST || !ok {
		return
	}
	for _, v := range s.Values {
		if lit, ok := v.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if val, err := strconv.Unquote(lit.Value); err == nil {
				p.consts[typ.Name] = append(p.consts[typ.Name], val)
			}
		}
	}
}

// exported 判断一个类型是否生成：字符串类型，或至少有一个 json 标签的结构体。
func exported(p *pkgInfo, name string) bool {
	s := p.types[name]
	if s == nil || !ast.IsExported(name) {
		return false
	}
	if id, ok := s.Type.(*ast.Ident); ok {
		return id.Name == "string"
	}
	st, ok := s.Type.(*ast.StructType)
	if !ok {
		return false
	}
	for _, f := range st.Fields.List {
		if f.Tag != nil && jsonTag(f) != "" {
			return true
		}
	}
	return false
}

func render(p *pkgInfo, pkgs map[string]*pkgInfo) (string, error) {
	r := &renderer{p: p, pkgs: pkgs, imports: map[string]bool{}}
	var body strings.Builder
	for _, name := range p.order {
		if !exported(p, name) {
			continue
		}
		writeDoc(&body, p.docs[name], "")
		if vals := p.consts[name]; len(vals) > 0 {
			quoted := make([]string, len(vals))
			for i, v := range vals {
				quoted[i] = strconv.Quote(v)
			}
			fmt.Fprintf(&body, "export type %s = %s;\n\n", name, strings.Join(quoted, " | "))
			continue
		}
		if _, ok := p.types[name].Type.(*ast.Ident); ok {
			fmt.Fprintf(&body, "export type %s = string;\n\n", name)
			continue
		}
		fmt.Fprintf(&body, "export interface %s {\n", name)
		if err := r.fields(&body, p, p.types[name].Type.(*ast.StructType)); err != nil {
			return "", fmt.Errorf("%s：%w", name, err)
		}
		body.WriteString("}\n\n")
	}
	var sb strings.Builder
	sb.WriteString(header)
	fmt.Fprintf(&sb, "// 来源：%s\n\n", p.dir)
	var imps []string
	for imp := range r.imports {
		imps = append(imps, imp)
	}
	sort.Strings(imps)
	for _, imp := range imps {
		fmt.Fprintf(&sb, "import type * as %s from \"./%s\";\n", imp, imp)
	}
	if len(imps) > 0 {
		sb.WriteString("\n")
	}
	sb.WriteString(strings.TrimRight(body.String(), "\n") + "\n")
	return sb.String(), nil
}

type renderer struct {
	p       *pkgInfo // 正在生成的包
	pkgs    map[string]*pkgInfo
	imports map[string]bool
}

// fields 输出 st 的字段。owner 是 st 所在的包，嵌入字段可能来自其他包。
func (r *renderer) fields(sb *strings.Builder, owner *pkgInfo, st *ast.StructType) error {
	for _, f := range st.Fields.List {
		tag := jsonTag(f)
		if tag == "-" {
			continue
		}
		if len(f.Names) == 0 && tag == "" {
			emb, err := r.embedded(owner, f.Type)
			if err != nil {
				return err
			}
			if err := r.fields(sb, emb.pkg, emb.st); err != nil {
				return err
			}
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		optional := strings.Contains(","+opts+",", ",omitempty,")
		goNames := f.Names
		if len(goNames) == 0 {
			goNames = []*ast.Ident{ast.NewIdent(typeName(f.Type))}
		}
		for _, id := range goNames {
			if !ast.IsExported(id.Name) {
				continue
			}
			key := name
			if key == "" {
				key = id.Name
			}
			ts, err := r.tsType(owner, f.Type, optional)
			if err != nil {
				return fmt.Errorf("字段 %s：%w", id.Name, err)
			}
			writeDoc(sb, f.Doc.Text(), "  ")
			q := ""
			if optional {
				q = "?"
			}
			fmt.Fprintf(sb, "  %s%s: %s;", key, q, ts)
			if c := strings.TrimSpace(f.Comment.Text()); c != "" {
				fmt.Fprintf(sb, " // %s", strings.ReplaceAll(c, "\n", " "))
			}
			sb.WriteString("\n")
		}
	}
	return nil
}

type embeddedStruct struct {
	pkg *pkgInfo
	st  *ast.StructType
}

func (r *renderer) embedded(owner *pkgInfo, expr ast.Expr) (embeddedStruct, error) {
	pkg, name := owner, ""
	switch e := expr.(type) {
	case *ast.Ident:
		name = e.Name
	case *ast.SelectorExpr:
		x, _ := e.X.(*ast.Ident)
		if x == nil || r.pkgs[x.Name] == nil {
			return embeddedStruct{}, fmt.Errorf("不支持嵌入 %s", typeName(expr))
		}
		pkg, name = r.pkgs[x.Name], e.Sel.Name
	default:
		return embeddedStruct{}, fmt.Errorf("不支持嵌入 %s", typeName(expr))
	}
	s := pkg.types[name]
	if s == nil {
		return embeddedStruct{}, fmt.Errorf("没有找到嵌入的 %s", name)
	}
	st, ok := s.Type.(*ast.StructType)
	if !ok {
		return embeddedStruct{}, fmt.Errorf("嵌入的 %s 不是结构体", name)
	}
	return embeddedStruct{pkg, st}, nil
}

func (r *renderer) tsType(owner *pkgInfo, expr ast.Expr, optional bool) (string, error) {
	switch e := expr.(type) {
	case *ast.Ident:
		switch e.Name {
		case "string":
			return "string", nil
		case "bool":
			return "boolean", nil
		case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float32", "float64":
			return "number", nil
		}
		if !exported(owner, e.Name) {
			return "", fmt.Errorf("类型 %s 没有生成", e.Name)
		}
		return r.ref(owner, e.Name), nil
	case *ast.SelectorExpr:
		x, _ := e.X.(*ast.Ident)
		if x != nil && x.Name == "time" && e.Sel.Name == "Time" {
			return "string", nil
		}
		if x == nil || r.pkgs[x.Name] == nil || !exported(r.pkgs[x.Name], e.Sel.Name) {
			return "", fmt.Errorf("类型 %s 没有生成", typeName(expr))
		}
		return r.ref(r.pkgs[x.Name], e.Sel.Name), nil
	case *ast.StarExpr:
		t, err := r.tsType(owner, e.X, optional)
		if err != nil || optional {
			return t, err
		}
		return t + " | null", nil
	case *ast.ArrayType:
		if id, ok := e.Elt.(*ast.Ident); ok && id.Name == "byte" {
			return "string", nil
		}
		t, err := r.tsType(owner, e.Elt, false)
		if strings.Contains(t, " ") {
			t = "(" + t + ")"
		}
		return t + "[]", err
	case *ast.MapType:
		if k, ok := e.Key.(*ast.Ident); !ok || k.Name != "string" {
			return "", fmt.Errorf("map 的键必须是 string")
		}
		v, err := r.tsType(owner, e.Value, false)
		return "Record<string, " + v + ">", err
	}
	return "", fmt.Errorf("不支持的类型 %s", typeName(expr))
}

// ref 返回类型在当前 TS 文件中的写法。其他包的类型带包名前缀。
func (r *renderer) ref(pkg *pkgInfo, name string) string {
	if pkg == r.p {
		return name
	}
	r.imports[pkg.name] = true
	return pkg.name + "." + name
}

func jsonTag(f *ast.Field) string {
	if f.Tag == nil {
		return ""
	}
	raw, err := strconv.Unquote(f.Tag.Value)
	if err != nil {
		return ""
	}
	return reflect.StructTag(raw).Get("json")
}

func typeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return typeName(e.X) + "." + e.Sel.Name
	case *ast.StarExpr:
		return typeName(e.X)
	}
	return fmt.Sprintf("%T", expr)
}

func writeDoc(sb *strings.Builder, doc, indent string) {
	for _, line := range strings.Split(strings.TrimSpace(doc), "\n") {
		if line != "" {
			fmt.Fprintf(sb, "%s// %s\n", indent, line)
		}
	}
}
