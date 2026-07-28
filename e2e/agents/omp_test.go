package agents

import (
	"regexp"
	"testing"
)

func TestOMPBusyPattern(t *testing.T) {
	t.Parallel()

	re := regexp.MustCompile(ompBusyPattern)
	presets := []struct {
		name   string
		frames []string
		hint   string
	}{
		{"unicode", []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}, "⟦esc⟧"},
		{"nerd", []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}, "⟨esc⟩"},
		{"ascii", []string{"-", `\`, "|", "/"}, "[esc]"},
	}
	for _, preset := range presets {
		for _, frame := range preset.frames {
			t.Run(preset.name+"/"+frame, func(t *testing.T) {
				t.Parallel()

				line := "\t" + frame + " Working… " + preset.hint + "  "
				if !re.MatchString(line) {
					t.Fatalf("MatchString(%q) = false, want true", line)
				}
			})
		}
	}

	tests := []struct {
		name string
		line string
		busy bool
	}{
		{"unicode hint in body", "The user typed ⟦esc⟧", false},
		{"nerd hint in body", "The assistant typed ⟨esc⟩", false},
		{"ascii hint in body", "The user typed [esc]", false},
		{"bare unicode hint", "⟦esc⟧", false},
		{"bare nerd hint", "⟨esc⟩", false},
		{"bare ascii hint", "[esc]", false},
		{"prompt", "╭── [esc]", false},
		{"spinner without hint", "⠋ Working…", false},
		{"text after hint", "⠋ Working… ⟦esc⟧ still ordinary text", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := re.MatchString(tt.line); got != tt.busy {
				t.Fatalf("MatchString(%q) = %t, want %t", tt.line, got, tt.busy)
			}
		})
	}
}
