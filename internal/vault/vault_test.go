package vault

import (
	"errors"
	"strings"
	"testing"
)

const fakeSecret = "fake-vault-secret-1234"

func TestMissingSecretIsNotReturned(t *testing.T) {
	if s, err := Get("fake-missing"); err == nil || s != "" {
		t.Fatalf("Get(fake-missing) = %q, %v; want empty, error", s, err)
	}
	if h, err := Hint("fake-missing"); err == nil || h != "" {
		t.Fatalf("Hint(fake-missing) = %q, %v; want empty, error", h, err)
	}
}

// 不合法的供应商名称在调用钥匙串前被拒绝。返回 errBadProvider，说明没有走到平台实现。
func TestRejectBadProvider(t *testing.T) {
	for _, p := range []string{"", "Anthropic", "fake provider", "fake:x", "fake/x", `fake\x`, "../fake", "fake_x", "假的", strings.Repeat("a", 65)} {
		if err := Set(p, fakeSecret); !errors.Is(err, errBadProvider) {
			t.Errorf("Set(%q) = %v", p, err)
		}
		if _, err := Get(p); !errors.Is(err, errBadProvider) {
			t.Errorf("Get(%q) = %v", p, err)
		}
		if err := Delete(p); !errors.Is(err, errBadProvider) {
			t.Errorf("Delete(%q) = %v", p, err)
		}
		if _, err := Hint(p); !errors.Is(err, errBadProvider) {
			t.Errorf("Hint(%q) = %v", p, err)
		}
	}
	if err := check("fake-provider-2"); err != nil {
		t.Errorf("合法的名称被拒绝：%v", err)
	}
}

func TestSetRejectsEmptySecret(t *testing.T) {
	if err := Set("fake-provider", ""); !errors.Is(err, errEmpty) {
		t.Fatalf("Set 空密钥 = %v", err)
	}
}

func TestHint(t *testing.T) {
	for secret, want := range map[string]string{
		fakeSecret: "1234",
		"abc":      "abc",
		"假的密钥一二三四": "一二三四",
		"":         "",
	} {
		if got := hint(secret); got != want {
			t.Errorf("hint(%q) = %q, want %q", secret, got, want)
		}
	}
}

func TestTarget(t *testing.T) {
	if got := target("anthropic"); got != "airlock:anthropic" {
		t.Fatalf("target = %q", got)
	}
}

func TestErrorsDoNotContainSecret(t *testing.T) {
	for _, err := range []error{Set("Bad", fakeSecret), Set("fake-provider", "")} {
		if err != nil && strings.Contains(err.Error(), fakeSecret) {
			t.Errorf("错误信息中有密钥：%v", err)
		}
	}
}
