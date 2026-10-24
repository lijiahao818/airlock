package vault

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// fakeProvider 返回一个随机的测试供应商名称，测试结束时删除它的条目。
func fakeProvider(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	rand.Read(b)
	p := "fake-test-" + hex.EncodeToString(b)
	t.Cleanup(func() { Delete(p) })
	return p
}

func TestRoundTrip(t *testing.T) {
	p := fakeProvider(t)
	for _, secret := range []string{fakeSecret, "fake-假的密钥-5678"} {
		if err := Set(p, secret); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if got, err := Get(p); err != nil || got != secret {
			t.Fatalf("Get = %q, %v; want %q", got, err, secret)
		}
	}
	if h, err := Hint(p); err != nil || h != "5678" {
		t.Fatalf("Hint = %q, %v", h, err)
	}
}

// 条目名为 airlock:<provider>，直接按这个名称能读到密钥。
func TestTargetName(t *testing.T) {
	p := fakeProvider(t)
	if err := Set(p, fakeSecret); err != nil {
		t.Fatal(err)
	}
	b, err := read("airlock:" + p)
	if err != nil || string(b) != fakeSecret {
		t.Fatalf("read(airlock:%s) = %q, %v", p, b, err)
	}
	if _, err := read(p); !errors.Is(err, ErrNotFound) {
		t.Fatalf("没有前缀的条目名也能读到：%v", err)
	}
}

func TestNotFound(t *testing.T) {
	p := fakeProvider(t)
	if _, err := Get(p); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get = %v", err)
	}
	if _, err := Hint(p); !errors.Is(err, ErrNotFound) {
		t.Errorf("Hint = %v", err)
	}
	if err := Delete(p); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete = %v", err)
	}
	if err := Set(p, fakeSecret); err != nil {
		t.Fatal(err)
	}
	if err := Delete(p); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := Get(p); !errors.Is(err, ErrNotFound) {
		t.Errorf("删除后 Get = %v", err)
	}
}

// 凭据管理器拒绝过长的密钥时，错误信息中不能有密钥。
func TestWriteErrorDoesNotContainSecret(t *testing.T) {
	p := fakeProvider(t)
	long := "fake-" + strings.Repeat("x", 3000)
	err := Set(p, long)
	if err == nil {
		t.Fatal("超过 2560 字节的密钥被保存了")
	}
	if strings.Contains(err.Error(), "xxxx") {
		t.Fatalf("错误信息中有密钥：%v", err)
	}
}
