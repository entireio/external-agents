//go:build windows

package agents

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/UserExistsError/conpty"
)

type ConPTYSession struct {
	cpty     *conpty.ConPty
	cancel   context.CancelFunc
	procDone chan struct{}

	mu       sync.Mutex
	screen   *renderedScreen
	rawBuf   []byte
	exitErr  error
	cleanups []func()

	stableAtSend string
}

func NewConPTYSession(name string, dir string, unsetEnv []string, command string, args ...string) (*ConPTYSession, error) {
	_ = name

	env := filterEnv(os.Environ(), unsetEnv...)
	cmdLine := windowsCommandLine(command, args...)
	cpty, err := conpty.Start(
		cmdLine,
		conpty.ConPtyWorkDir(dir),
		conpty.ConPtyEnv(env),
		conpty.ConPtyDimensions(160, 48),
	)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &ConPTYSession{
		cpty:     cpty,
		cancel:   cancel,
		procDone: make(chan struct{}),
		screen:   newRenderedScreen(),
	}

	go s.readLoop()
	go s.waitLoop(ctx)

	return s, nil
}

func (s *ConPTYSession) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := s.cpty.Read(buf)
		if n > 0 {
			s.mu.Lock()
			s.rawBuf = appendCapped(s.rawBuf, buf[:n], 64*1024)
			_, _ = s.screen.Write(buf[:n])
			s.mu.Unlock()
		}
		if err != nil {
			if err == io.EOF {
				return
			}
			s.mu.Lock()
			if s.exitErr == nil {
				s.exitErr = err
			}
			s.mu.Unlock()
			return
		}
	}
}

func (s *ConPTYSession) waitLoop(ctx context.Context) {
	_, err := s.cpty.Wait(ctx)
	s.mu.Lock()
	if err != nil && s.exitErr == nil {
		s.exitErr = err
	}
	s.mu.Unlock()
	close(s.procDone)
}

func (s *ConPTYSession) Send(input string) error {
	preSend := stableContent(s.Capture())
	if err := s.SendKeys(input); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	if err := s.SendKeys("Enter"); err != nil {
		return err
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		current := stableContent(s.Capture())
		if current != preSend {
			s.stableAtSend = current
			return nil
		}
	}
	s.stableAtSend = stableContent(s.Capture())
	return nil
}

func (s *ConPTYSession) SendKeys(keys ...string) error {
	for _, key := range keys {
		if _, err := io.WriteString(s.cpty, translateKey(key)); err != nil {
			return err
		}
	}
	return nil
}

func (s *ConPTYSession) WaitFor(pattern string, timeout time.Duration) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid pattern: %w", err)
	}

	deadline := time.Now().Add(timeout)
	var matchedAt time.Time
	var lastStable string
	contentChanged := s.stableAtSend == ""

	for time.Now().Before(deadline) {
		content := s.Capture()
		stable := stableContent(content)

		if !re.MatchString(content) {
			select {
			case <-s.procDone:
				return content, fmt.Errorf("process exited while waiting for %q\n--- pane content ---\n%s\n--- end pane content ---", pattern, content)
			default:
			}
			matchedAt = time.Time{}
			lastStable = ""
			time.Sleep(pollInterval)
			continue
		}

		if !contentChanged && stable != s.stableAtSend {
			contentChanged = true
		}

		if stable != lastStable {
			matchedAt = time.Now()
			lastStable = stable
			time.Sleep(pollInterval)
			continue
		}

		if contentChanged && time.Since(matchedAt) >= settleTime {
			return content, nil
		}

		time.Sleep(pollInterval)
	}

	content := s.Capture()
	return content, fmt.Errorf("timed out waiting for %q after %s\n--- pane content ---\n%s\n--- end pane content ---", pattern, timeout, content)
}

func (s *ConPTYSession) Capture() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimRight(s.screen.String(), "\n")
}

func (s *ConPTYSession) Close() error {
	s.cancel()
	for _, fn := range s.cleanups {
		fn()
	}
	return s.cpty.Close()
}

func appendCapped(dst []byte, src []byte, maxSize int) []byte {
	dst = append(dst, src...)
	if len(dst) <= maxSize {
		return dst
	}
	drop := len(dst) - (maxSize * 3 / 4)
	if drop < 0 {
		drop = 0
	}
	return append([]byte(nil), dst[drop:]...)
}

func windowsCommandLine(command string, args ...string) string {
	parts := []string{quoteWindowsArg(command)}
	for _, arg := range args {
		parts = append(parts, quoteWindowsArg(arg))
	}
	return strings.Join(parts, " ")
}

func quoteWindowsArg(arg string) string {
	if arg == "" {
		return `""`
	}
	if !strings.ContainsAny(arg, " \t\"") {
		return arg
	}
	return `"` + strings.ReplaceAll(arg, `"`, `\"`) + `"`
}
