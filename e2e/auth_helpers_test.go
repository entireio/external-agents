//go:build e2e

package e2e

import (
	"testing"

	"github.com/entireio/external-agents/e2e/agents"
)

func TestShouldSkipAuthGatedTests(t *testing.T) {
	t.Setenv("E2E_NO_AUTH", "1")
	if !shouldSkipAuthGatedTests() {
		t.Fatal("shouldSkipAuthGatedTests() = false, want true")
	}
}

func TestShouldSkipAuthGatedTestsLocalDefault(t *testing.T) {
	t.Setenv("E2E_NO_AUTH", "")
	if shouldSkipAuthGatedTests() {
		t.Fatal("shouldSkipAuthGatedTests() = true, want false")
	}
}

func TestInteractiveSessionSkipReasonForKiroAPIKeyMode(t *testing.T) {
	t.Setenv("E2E_API_KEY_AUTH", "1")
	reason := interactiveSessionSkipReason(&agents.Kiro{})
	if reason == "" {
		t.Fatal("interactiveSessionSkipReason(Kiro) = empty, want skip reason")
	}
}

func TestInteractiveSessionSkipReasonForPiAPIKeyMode(t *testing.T) {
	t.Setenv("E2E_API_KEY_AUTH", "1")
	reason := interactiveSessionSkipReason(&agents.Pi{})
	if reason != "" {
		t.Fatalf("interactiveSessionSkipReason(Pi) = %q, want empty", reason)
	}
}
