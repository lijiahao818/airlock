package main

import (
	"slices"
	"testing"

	"github.com/lijiahao818/airlock/internal/gate"
	"github.com/lijiahao818/airlock/presets"
)

// presets 只用标准库，不导入 gate，所以在入口层核对：每个预设的供应商都是闸门的模型路由。
// gate 删除或改名一个供应商时，这个测试失败，不会等到运行时才发现。
func TestPresetProvidersAreGateProviders(t *testing.T) {
	all, err := presets.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range all {
		if !slices.Contains(gate.Providers(), p.Provider) {
			t.Errorf("%s 的供应商 %q 不在 gate.Providers() %q 中", p.ID, p.Provider, gate.Providers())
		}
	}
}
