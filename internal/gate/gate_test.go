package gate

import (
	"context"
	"reflect"
	"testing"
)

func TestContractValues(t *testing.T) {
	if got, want := Providers(), []string{"anthropic", "openai", "deepseek"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Providers() = %v, want %v", got, want)
	}
	if got, want := Tiers(), []string{"strict", "standard", "permissive"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Tiers() = %v, want %v", got, want)
	}
}

func TestRejectUnknownTier(t *testing.T) {
	if hosts, err := TierHosts("fake-tier"); err == nil || hosts != nil {
		t.Fatalf("TierHosts(fake-tier) = %v, %v; want nil, error", hosts, err)
	}
}

// 未知供应商必须在发出网络请求前被拒绝。
func TestRejectUnknownProviderKey(t *testing.T) {
	if err := TestKey(context.Background(), "fake-provider", "fake-key"); err == nil {
		t.Fatal("TestKey 接受了未知的供应商")
	}
}
