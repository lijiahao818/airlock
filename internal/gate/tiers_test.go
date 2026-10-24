package gate

import (
	"slices"
	"testing"
)

func TestTierHostsAreNested(t *testing.T) {
	var prev []string
	for _, tier := range Tiers() {
		hosts, err := TierHosts(tier)
		if err != nil {
			t.Fatalf("TierHosts(%s): %v", tier, err)
		}
		for _, h := range prev {
			if !slices.Contains(hosts, h) {
				t.Errorf("%s 档位缺少上一档位的主机 %s", tier, h)
			}
		}
		if len(hosts) <= len(prev) {
			t.Errorf("%s 档位没有比上一档位多放行主机", tier)
		}
		prev = hosts
	}
}

func TestStrictTierOnlyModelHosts(t *testing.T) {
	hosts, _ := TierHosts("strict")
	if want := []string{"api.anthropic.com", "api.openai.com", "api.deepseek.com"}; !slices.Equal(hosts, want) {
		t.Fatalf("TierHosts(strict) = %v, want %v", hosts, want)
	}
}

// 调用方修改返回值时，不能改动档位表。
func TestTierHostsReturnsCopy(t *testing.T) {
	hosts, _ := TierHosts("permissive")
	hosts[0] = "fake-evil.example"
	again, _ := TierHosts("permissive")
	if again[0] == "fake-evil.example" {
		t.Fatal("TierHosts 返回了档位表本身")
	}
}
