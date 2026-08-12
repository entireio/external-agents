package hermes

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	pluginName   = "entire-observer"
	pluginMarker = "entire-agent-hermes observer v1"
)

var observerHooks = []string{
	"on_session_start",
	"pre_llm_call",
	"pre_tool_call",
	"post_tool_call",
	"post_llm_call",
	"on_session_end",
	"on_session_finalize",
}

//go:embed plugin/plugin.yaml plugin/__init__.py
var pluginFiles embed.FS

type repositoryRegistration struct {
	Path      string `json:"path"`
	EntireBin string `json:"entire_bin"`
}

type repositoryRegistry struct {
	Version      int                      `json:"version"`
	Repositories []repositoryRegistration `json:"repositories"`
}

func pluginDir(home string) string { return filepath.Join(home, "plugins", pluginName) }

func registryPath(home string) string { return filepath.Join(pluginDir(home), "repositories.json") }

func loadRegistry(home string) (repositoryRegistry, error) {
	registry := repositoryRegistry{Version: 1, Repositories: []repositoryRegistration{}}
	data, err := os.ReadFile(registryPath(home))
	if errors.Is(err, os.ErrNotExist) {
		return registry, nil
	}
	if err != nil {
		return registry, fmt.Errorf("read Hermes observer registry: %w", err)
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		return registry, fmt.Errorf("parse Hermes observer registry: %w", err)
	}
	if registry.Version != 1 {
		return registry, fmt.Errorf("unsupported Hermes observer registry version %d", registry.Version)
	}
	if registry.Repositories == nil {
		registry.Repositories = []repositoryRegistration{}
	}
	return registry, nil
}

func saveRegistry(home string, registry repositoryRegistry) error {
	registry.Version = 1
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(registryPath(home), data, 0o600)
}

func resolvedEntireBinary() (string, error) {
	path, err := exec.LookPath("entire")
	if err != nil {
		return "", errors.New("entire executable was not found in PATH")
	}
	return canonicalPath(path)
}

func ensureOwnedOrMissing(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect existing Hermes plugin file %s: %w", path, err)
	}
	if !strings.Contains(string(data), pluginMarker) {
		return fmt.Errorf("refusing to overwrite non-owned Hermes plugin file %s", path)
	}
	return nil
}

func (a *Agent) InstallHooks(_ bool, force bool) (int, error) {
	home, err := explicitHome()
	if err != nil {
		return 0, err
	}
	repo, err := canonicalPath(protocolRepoRoot())
	if err != nil {
		return 0, fmt.Errorf("resolve repository: %w", err)
	}
	entireBin, err := resolvedEntireBinary()
	if err != nil {
		return 0, err
	}
	registry, err := loadRegistry(home)
	if err != nil {
		return 0, err
	}

	changed := force
	for _, name := range []string{"plugin.yaml", "__init__.py"} {
		if err := ensureOwnedOrMissing(filepath.Join(pluginDir(home), name)); err != nil {
			return 0, err
		}
	}
	for _, name := range []string{"plugin.yaml", "__init__.py"} {
		destination := filepath.Join(pluginDir(home), name)
		data, readErr := fs.ReadFile(pluginFiles, "plugin/"+name)
		if readErr != nil {
			return 0, readErr
		}
		fileChanged, writeErr := writeIfChanged(destination, data, 0o600, force)
		if writeErr != nil {
			return 0, fmt.Errorf("install Hermes observer %s: %w", name, writeErr)
		}
		changed = fileChanged || changed
	}

	found := false
	for i := range registry.Repositories {
		if registry.Repositories[i].Path == repo {
			found = true
			if registry.Repositories[i].EntireBin != entireBin {
				registry.Repositories[i].EntireBin = entireBin
				changed = true
			}
			break
		}
	}
	if !found {
		registry.Repositories = append(registry.Repositories, repositoryRegistration{Path: repo, EntireBin: entireBin})
		changed = true
	}
	if changed {
		if err := saveRegistry(home, registry); err != nil {
			return 0, fmt.Errorf("register repository with Hermes observer: %w", err)
		}
	}
	configChanged, err := updatePluginConfig(home, true)
	if err != nil {
		return 0, err
	}
	changed = configChanged || changed
	if !changed {
		return 0, nil
	}
	return len(observerHooks), nil
}

func (a *Agent) UninstallHooks() error {
	home, err := explicitHome()
	if err != nil {
		return err
	}
	repo, err := canonicalPath(protocolRepoRoot())
	if err != nil {
		return err
	}
	registry, err := loadRegistry(home)
	if err != nil {
		return err
	}
	remaining := registry.Repositories[:0]
	for _, entry := range registry.Repositories {
		if entry.Path != repo {
			remaining = append(remaining, entry)
		}
	}
	registry.Repositories = remaining
	if len(remaining) > 0 {
		return saveRegistry(home, registry)
	}

	if _, err := updatePluginConfig(home, false); err != nil {
		return err
	}
	for _, path := range []string{
		filepath.Join(pluginDir(home), "plugin.yaml"),
		filepath.Join(pluginDir(home), "__init__.py"),
		registryPath(home),
	} {
		if err := removeOwnedFile(path); err != nil {
			return err
		}
	}
	if err := os.Remove(pluginDir(home)); err != nil && !errors.Is(err, os.ErrNotExist) && !isNotEmpty(err) {
		return err
	}
	return nil
}

func (a *Agent) AreHooksInstalled() bool {
	home, err := explicitHome()
	if err != nil || !pluginEnabled(home) {
		return false
	}
	repo, err := canonicalPath(protocolRepoRoot())
	if err != nil {
		return false
	}
	registry, err := loadRegistry(home)
	if err != nil {
		return false
	}
	found := false
	for _, entry := range registry.Repositories {
		if entry.Path == repo && entry.EntireBin != "" {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	for _, name := range []string{"plugin.yaml", "__init__.py"} {
		data, err := os.ReadFile(filepath.Join(pluginDir(home), name))
		if err != nil || !strings.Contains(string(data), pluginMarker) {
			return false
		}
	}
	return true
}

func protocolRepoRoot() string {
	if root := strings.TrimSpace(os.Getenv("ENTIRE_REPO_ROOT")); root != "" {
		return root
	}
	root, _ := os.Getwd()
	return root
}

func writeIfChanged(path string, data []byte, mode os.FileMode, force bool) (bool, error) {
	existing, err := os.ReadFile(path)
	if err == nil && string(existing) == string(data) && !force {
		return false, os.Chmod(path, mode)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return true, atomicWrite(path, data, mode)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".entire-agent-hermes-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		return closeTemporary(tmp, err)
	}
	if _, err := tmp.Write(data); err != nil {
		return closeTemporary(tmp, err)
	}
	if err := tmp.Sync(); err != nil {
		return closeTemporary(tmp, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func closeTemporary(file *os.File, cause error) error {
	if closeErr := file.Close(); closeErr != nil {
		return errors.Join(cause, closeErr)
	}
	return cause
}

func removeOwnedFile(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if filepath.Base(path) != "repositories.json" && !strings.Contains(string(data), pluginMarker) {
		return fmt.Errorf("refusing to remove modified non-owned file %s", path)
	}
	return os.Remove(path)
}

func isNotEmpty(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "not empty")
}
