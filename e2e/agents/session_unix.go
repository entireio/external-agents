//go:build !windows

package agents

func newInteractiveSession(name, dir string, unsetEnv []string, command string, args ...string) (Session, error) {
	return NewTmuxSession(name, dir, unsetEnv, command, args...)
}
