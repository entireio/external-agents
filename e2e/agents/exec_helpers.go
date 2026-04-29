package agents

import (
	"fmt"
	"os"
	"os/exec"
)

func lookPathAny(names ...string) (string, error) {
	var lastErr error
	for _, name := range names {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = exec.ErrNotFound
	}
	return "", fmt.Errorf("%v not found in PATH: %w", names, lastErr)
}

func isAPIKeyAuthMode() bool {
	return os.Getenv("E2E_API_KEY_AUTH") == "1"
}

func requireEnv(name string) error {
	if os.Getenv(name) == "" {
		return fmt.Errorf("%s is required", name)
	}
	return nil
}

func requireAnyEnv(names ...string) error {
	for _, name := range names {
		if os.Getenv(name) != "" {
			return nil
		}
	}
	return fmt.Errorf("one of %v is required", names)
}
