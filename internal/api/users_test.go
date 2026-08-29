package api

import (
	"strings"
	"testing"
)

func TestNormalizeAllowedAppIDs(t *testing.T) {
	got, code, message := normalizeAllowedAppIDs([]string{" app.two ", "", "app.one", "app.two"})
	if code != "" || message != "" {
		t.Fatalf("unexpected validation error: %s %s", code, message)
	}
	if len(got) != 2 || got[0] != "app.one" || got[1] != "app.two" {
		t.Fatalf("normalized ids = %#v", got)
	}

	_, code, _ = normalizeAllowedAppIDs([]string{strings.Repeat("x", 257)})
	if code != "invalid_app_id" {
		t.Fatalf("long app id error code = %q", code)
	}
}
