package agents

import "testing"

func TestKiroBootstrapRequiresAPIKeyInHeadlessMode(t *testing.T) {
	t.Setenv("E2E_API_KEY_AUTH", "1")
	t.Setenv("KIRO_API_KEY", "")

	if err := (&Kiro{}).Bootstrap(); err == nil {
		t.Fatal("Kiro.Bootstrap() error = nil, want non-nil")
	}
}

func TestPiBootstrapRequiresProviderKeyInHeadlessMode(t *testing.T) {
	t.Setenv("E2E_API_KEY_AUTH", "1")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	if err := (&Pi{}).Bootstrap(); err == nil {
		t.Fatal("Pi.Bootstrap() error = nil, want non-nil")
	}
}

func TestPiBootstrapAcceptsProviderKeyInHeadlessMode(t *testing.T) {
	t.Setenv("E2E_API_KEY_AUTH", "1")
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	if err := (&Pi{}).Bootstrap(); err != nil {
		t.Fatalf("Pi.Bootstrap() error = %v, want nil", err)
	}
}
