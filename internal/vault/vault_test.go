package vault

import "testing"

func TestMissingSecretIsNotReturned(t *testing.T) {
	if s, err := Get("fake-missing"); err == nil || s != "" {
		t.Fatalf("Get(fake-missing) = %q, %v; want empty, error", s, err)
	}
	if h, err := Hint("fake-missing"); err == nil || h != "" {
		t.Fatalf("Hint(fake-missing) = %q, %v; want empty, error", h, err)
	}
}
