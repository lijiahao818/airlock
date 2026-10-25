package cabin

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// tree 读取 dir 下全部文件的内容，按斜杠分隔的相对路径索引。
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		rel, _ := filepath.Rel(dir, p)
		got[filepath.ToSlash(rel)] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// #80 AC-1、AC-2：删除空闲的工作舱后，它不在列表中，缓存卷和暂存区都删除，项目文件夹不变。重启后仍然不在列表中。
func TestDelete(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, nil, nil)
	c := e.started(t, "claude-code")
	e.m.Stop(ctx, c.ID)
	if err := e.m.FinishReview(c.ID); err != nil {
		t.Fatal(err)
	}
	keep := e.create(t, "claude-code")
	// 模拟 FinishReview 没有删掉的暂存区。
	writeFile(t, filepath.Join(e.m.areaDir(c.ID), "files", "main.go"), "fake-left\n")
	before := tree(t, c.Project)

	if err := e.m.Delete(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if e.b.called("remove-cache:"+c.ID) != 1 {
		t.Fatalf("没有删除缓存卷：%q", e.b.calls)
	}
	if _, err := os.Stat(e.m.areaDir(c.ID)); !os.IsNotExist(err) {
		t.Fatal("暂存区没有删除")
	}
	if got := tree(t, c.Project); !reflect.DeepEqual(got, before) {
		t.Fatalf("项目文件夹被修改：%v", got)
	}
	list, _ := e.m.List()
	if len(list) != 1 || list[0].ID != keep.ID {
		t.Fatalf("List = %+v，want 只剩 %s", list, keep.ID)
	}
	m2, err := New(Options{DataDir: e.dataDir, Backend: e.b, Secret: e.m.o.Secret})
	if err != nil {
		t.Fatal(err)
	}
	if list, _ := m2.List(); len(list) != 1 || list[0].ID != keep.ID {
		t.Fatalf("重启后 List = %+v", list)
	}
}

// #80 AC-3：运行中和等待出舱审查的工作舱不能删除。返回中文错误，工作舱和资源都保留。不存在的工作舱也返回错误。
func TestDeleteRejects(t *testing.T) {
	ctx := context.Background()
	for _, from := range []Status{Running, Review} {
		t.Run(string(from), func(t *testing.T) {
			e := newEnv(t, nil, nil)
			c := e.started(t, "claude-code")
			if from == Review {
				e.m.Stop(ctx, c.ID)
			}
			err := e.m.Delete(ctx, c.ID)
			if err == nil || !strings.Contains(err.Error(), "不能删除") {
				t.Fatalf("Delete = %v，want 中文错误", err)
			}
			if e.status(t, c.ID) != from {
				t.Fatal("拒绝删除后状态改变了")
			}
			if e.b.called("remove-cache:"+c.ID) != 0 {
				t.Fatal("拒绝删除时不能删除缓存卷")
			}
			if _, err := os.Stat(e.m.areaDir(c.ID)); err != nil {
				t.Fatalf("拒绝删除时暂存区不能删除：%v", err)
			}
		})
	}
	e := newEnv(t, nil, nil)
	if err := e.m.Delete(ctx, "nope"); err == nil {
		t.Fatal("删除不存在的工作舱应该返回错误")
	}
}

// 删除缓存卷、暂存区或保存列表失败时，工作舱保留，可以再次删除。
func TestDeleteFailures(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, nil, nil)
	c := e.create(t, "claude-code")
	kept := func() {
		t.Helper()
		if _, err := e.m.get(c.ID); err != nil {
			t.Fatal("删除失败后工作舱不能从列表中消失")
		}
	}

	e.b.cacheErr = errors.New("fake: 卷正在使用")
	if err := e.m.Delete(ctx, c.ID); err == nil || !strings.Contains(err.Error(), "删除依赖缓存失败") {
		t.Fatalf("Delete = %v", err)
	}
	kept()
	e.b.cacheErr = nil

	e.m.removeAll = func(string) error { return errors.New("fake: 目录被占用") }
	if err := e.m.Delete(ctx, c.ID); err == nil || !strings.Contains(err.Error(), "删除暂存区失败") {
		t.Fatalf("Delete = %v", err)
	}
	kept()
	e.m.removeAll = os.RemoveAll

	// 临时文件的位置被目录占用，保存列表失败。
	tmp := e.m.cabinsFile() + ".tmp"
	if err := os.Mkdir(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := e.m.Delete(ctx, c.ID); err == nil || !strings.Contains(err.Error(), "保存工作舱列表失败") {
		t.Fatalf("Delete = %v", err)
	}
	kept()
	os.Remove(tmp)

	if err := e.m.Delete(ctx, c.ID); err != nil {
		t.Fatalf("再次删除失败：%v", err)
	}
	if _, err := e.m.get(c.ID); err == nil {
		t.Fatal("工作舱仍在列表中")
	}
}
