package audit

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lijiahao818/airlock/internal/gate"
)

var fakeTime = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

func gateEntry(host, rule string, allowed bool) Entry {
	return Entry{Gate: &gate.Event{Time: fakeTime, Host: host, Port: 443, Method: "CONNECT", Rule: rule, Allowed: allowed}}
}

func reviewEntry(path string, applied bool) Entry {
	return Entry{Review: &Review{Time: fakeTime, Path: path, Applied: applied}}
}

func openLog(t *testing.T, path string) *Log {
	t.Helper()
	l, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return l
}

func add(t *testing.T, l *Log, entries ...Entry) {
	t.Helper()
	for _, e := range entries {
		if err := l.Add(e); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
}

func TestAddAndRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs", "fake-run.jsonl")
	l := openLog(t, path)
	want := []Entry{
		gateEntry("fake-site.example", "user-allow", true),
		{Gate: &gate.Event{Time: fakeTime, Host: "api.anthropic.com", Port: 443, Method: "POST", Path: "/anthropic/v1/messages", Rule: "route", Allowed: true, Provider: "anthropic"}},
		reviewEntry("src/main.go", true),
	}
	add(t, l, want...)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Read = %+v\nwant %+v", got, want)
	}
	data, _ := os.ReadFile(path)
	if n := bytes.Count(data, []byte("\n")); n != len(want) {
		t.Errorf("文件有 %d 行，want %d", n, len(want))
	}
}

func TestOpenAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake-run.jsonl")
	l := openLog(t, path)
	add(t, l, reviewEntry("a", true), reviewEntry("b", false))
	l.Close()
	l = openLog(t, path)
	add(t, l, reviewEntry("c", true))
	l.Close()
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, e := range got {
		paths = append(paths, e.Review.Path)
	}
	if !reflect.DeepEqual(paths, []string{"a", "b", "c"}) {
		t.Fatalf("重新打开后的记录 %v", paths)
	}
}

// 闸门会从多个连接同时写入，每一行都必须完整。
func TestConcurrentAdd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake-run.jsonl")
	l := openLog(t, path)
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() { l.Add(gateEntry("fake-site.example", "tier", true)) })
	}
	wg.Wait()
	l.Close()
	got, err := Read(path)
	if err != nil {
		t.Fatalf("并发写入后 Read: %v", err)
	}
	if len(got) != 50 {
		t.Fatalf("读到 %d 条，want 50", len(got))
	}
}

func TestReadSkipsBlankLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake-run.jsonl")
	os.WriteFile(path, []byte("\n{\"review\":{\"path\":\"a\"}}\n\n"), 0o600)
	got, err := Read(path)
	if err != nil || len(got) != 1 || got[0].Review.Path != "a" {
		t.Fatalf("Read = %+v, %v", got, err)
	}
}

func TestReadEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake-run.jsonl")
	os.WriteFile(path, nil, 0o600)
	got, err := Read(path)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("Read = %#v, %v；want 空切片", got, err)
	}
}

func TestReadReportsLineNumber(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake-run.jsonl")
	os.WriteFile(path, []byte("{\"review\":{\"path\":\"a\"}}\n\n{fake-broken\n"), 0o600)
	_, err := Read(path)
	if err == nil || !strings.Contains(err.Error(), "第 3 行") {
		t.Fatalf("Read = %v，want 第 3 行格式错误", err)
	}
}

func TestReadRejectsLongLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake-run.jsonl")
	os.WriteFile(path, bytes.Repeat([]byte("x"), maxLine+1), 0o600)
	if _, err := Read(path); err == nil {
		t.Fatal("超长的行没有报错")
	}
}

func TestReadMissingFile(t *testing.T) {
	if _, err := Read(filepath.Join(t.TempDir(), "fake-missing.jsonl")); err == nil {
		t.Fatal("文件不存在时 Read 没有报错")
	}
}

func TestOpenFails(t *testing.T) {
	file := filepath.Join(t.TempDir(), "fake-file")
	os.WriteFile(file, nil, 0o600)
	// 上级路径是文件，或者路径本身是目录。
	for _, path := range []string{filepath.Join(file, "run.jsonl"), t.TempDir()} {
		if l, err := Open(path); err == nil {
			l.Close()
			t.Errorf("Open(%s) 没有报错", path)
		}
	}
}

// JSON 不能表示年份超过 9999 的时间，Add 返回错误，不写入半行。
func TestAddRejectsUnencodable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake-run.jsonl")
	l := openLog(t, path)
	defer l.Close()
	if err := l.Add(Entry{Review: &Review{Time: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}}); err == nil {
		t.Fatal("无法编码的条目没有报错")
	}
	if data, _ := os.ReadFile(path); len(data) != 0 {
		t.Fatalf("文件中写入了 %q", data)
	}
}

func TestAddAfterClose(t *testing.T) {
	l := openLog(t, filepath.Join(t.TempDir(), "fake-run.jsonl"))
	l.Close()
	if err := l.Add(reviewEntry("a", true)); err == nil {
		t.Fatal("Close 之后 Add 没有报错")
	}
}
