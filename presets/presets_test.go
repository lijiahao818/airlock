package presets

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestClaudeCodeSampleMatchesPreset(t *testing.T) {
	data, err := os.ReadFile("claude-code/preset.json")
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var p Preset
	if err := dec.Decode(&p); err != nil {
		t.Fatalf("preset.json 不符合 Preset：%v", err)
	}
	want := Preset{
		ID:       "claude-code",
		Name:     "Claude Code",
		Provider: "anthropic",
		UI:       "terminal",
		Cmd:      []string{"claude"},
		Env: map[string]string{
			"ANTHROPIC_BASE_URL":                       "{gate}/anthropic",
			"ANTHROPIC_API_KEY":                        "{placeholder}",
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
			"HTTPS_PROXY":                              "{gate}",
		},
	}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("got %+v\nwant %+v", p, want)
	}
}
