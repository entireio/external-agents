package qwen

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestSessionFilenamesHashWindowsDevices(t *testing.T) {
	names := []string{"CON", "PRN", "AUX", "NUL"}
	for _, prefix := range []string{"COM", "LPT"} {
		for _, digit := range "0123456789¹²³" {
			names = append(names, prefix+string(digit))
		}
	}
	for _, name := range names {
		for _, id := range []string{name, strings.ToLower(name), name + ".txt", strings.ToLower(name) + ".tar.gz"} {
			t.Run(id, func(t *testing.T) {
				sum := sha256.Sum256([]byte(id))
				want := fmt.Sprintf("~%x", sum[:16])
				if got := safeFilename(id); got != want {
					t.Fatalf("safeFilename(%q) = %q, want %q", id, got, want)
				}
			})
		}
	}
	for _, id := range []string{"console", "NUL-session", "COM10", "LPT10", "COM1x", "foo.CON"} {
		if got := safeFilename(id); got != id {
			t.Errorf("benign %q changed to %q", id, got)
		}
	}
}
