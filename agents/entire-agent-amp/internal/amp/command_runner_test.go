package amp

import (
	"os"
	"testing"
)

func TestAmpEnvStripsBunVars(t *testing.T) {
	t.Setenv("BUN_BE_BUN", "1")
	t.Setenv("BUN_INSPECT", "0.0.0.0:9229")
	t.Setenv("BUN_DEBUG_QUIET_LOGS", "1")
	t.Setenv("BUN_INSTALL", "/keep/me")
	t.Setenv("KEEP_ME", "yes")
	env := ampEnv()
	stripped := []string{"BUN_BE_BUN", "BUN_INSPECT", "BUN_DEBUG_QUIET_LOGS"}
	for _, kv := range env {
		for _, bad := range stripped {
			if len(kv) > len(bad) && kv[:len(bad)+1] == bad+"=" {
				t.Fatalf("env still contains %q: %q", bad, kv)
			}
		}
	}
	wantPresent := map[string]string{"KEEP_ME=yes": "user env", "BUN_INSTALL=/keep/me": "non-bootstrap Bun var"}
	for kv, desc := range wantPresent {
		found := false
		for _, got := range env {
			if got == kv {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %s %q to be preserved", desc, kv)
		}
	}
	// sanity: actual process env hasn't been mutated
	if os.Getenv("KEEP_ME") != "yes" {
		t.Fatal("test setup error")
	}
}

func TestIsBunEnvVar(t *testing.T) {
	cases := map[string]bool{
		"BUN_BE_BUN":           true,
		"BUN_INSPECT":          true,
		"BUN_DEBUG_FOO":        true,
		"BUN_INSTALL":          false,
		"BUN_INSTALL_CACHE":    false,
		"PATH":                 false,
		"PLUGINS":              false,
		"BUN_INTERNAL_IPC_FD":  true,
	}
	for key, want := range cases {
		if got := isBunEnvVar(key); got != want {
			t.Errorf("isBunEnvVar(%q) = %v, want %v", key, got, want)
		}
	}
}
