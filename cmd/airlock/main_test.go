package main

import (
	"bytes"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name string
		args []string
		code int
		out  string
	}{
		{"version", []string{"version"}, 0, "dev\n"},
		{"no args", nil, 2, ""},
		{"unknown command", []string{"start"}, 2, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tt.args, &stdout, &stderr); code != tt.code {
				t.Fatalf("code = %d, want %d", code, tt.code)
			}
			if stdout.String() != tt.out {
				t.Fatalf("stdout = %q, want %q", stdout.String(), tt.out)
			}
			if tt.code != 0 && stderr.Len() == 0 {
				t.Fatal("stderr is empty on failure")
			}
		})
	}
}
