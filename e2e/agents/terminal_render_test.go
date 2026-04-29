package agents

import "testing"

func TestTranslateKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{name: "enter", key: "Enter", want: "\r"},
		{name: "tab", key: "Tab", want: "\t"},
		{name: "escape", key: "Esc", want: "\x1b"},
		{name: "ctrl", key: "C-c", want: "\x03"},
		{name: "literal", key: "hello", want: "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := translateKey(tt.key); got != tt.want {
				t.Fatalf("translateKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestRenderedScreenCarriageReturnRewrite(t *testing.T) {
	screen := newRenderedScreen()
	screen.WriteString("hello\r\x1b[Kbye")
	if got := screen.String(); got != "bye" {
		t.Fatalf("screen.String() = %q, want %q", got, "bye")
	}
}

func TestRenderedScreenCursorUpRewrite(t *testing.T) {
	screen := newRenderedScreen()
	screen.WriteString("first\nsecond\x1b[A\r\x1b[Kdone")
	if got := screen.String(); got != "done\nsecond" {
		t.Fatalf("screen.String() = %q, want %q", got, "done\nsecond")
	}
}
