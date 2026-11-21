package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Ramanan-Bala/donegate/shims"
)

const (
	shimFilename          = "donegate.ts"
	shimMarker            = "// donegate shim"
	shimBinaryPlaceholder = `"__DONEGATE_BIN__"`
	shimFileMode          = fs.FileMode(0o644)
)

// ShimOptions configures installing or uninstalling a harness plugin/extension shim.
type ShimOptions struct {
	Dir    string // target directory; defaults to the harness's user-level plugin/extension directory
	Binary string // absolute path to the donegate binary; default os.Executable() resolved
	DryRun bool
}

// DefaultOpenCodeDir returns the default user-level OpenCode plugins directory.
func DefaultOpenCodeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "opencode", "plugins"), nil
}

// DefaultPiDir returns the default user-level pi extensions directory.
func DefaultPiDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".pi", "agent", "extensions"), nil
}

// InstallOpenCode installs the embedded OpenCode plugin shim into o.Dir (or ~/.config/opencode/plugins).
func InstallOpenCode(o ShimOptions) (path string, changed bool, err error) {
	return installShim(shims.OpenCode, DefaultOpenCodeDir, o)
}

// InstallPi installs the embedded pi extension shim into o.Dir (or ~/.pi/agent/extensions).
func InstallPi(o ShimOptions) (path string, changed bool, err error) {
	return installShim(shims.Pi, DefaultPiDir, o)
}

// UninstallOpenCode removes the OpenCode shim if it exists and carries the donegate shim marker.
func UninstallOpenCode(o ShimOptions) (path string, changed bool, err error) {
	return uninstallShim(DefaultOpenCodeDir, o)
}

// UninstallPi removes the pi shim if it exists and carries the donegate shim marker.
func UninstallPi(o ShimOptions) (path string, changed bool, err error) {
	return uninstallShim(DefaultPiDir, o)
}

func installShim(template []byte, defaultDir func() (string, error), o ShimOptions) (string, bool, error) {
	dir, err := resolveShimDir(o.Dir, defaultDir)
	if err != nil {
		return "", false, err
	}

	binaryPath, err := resolveBinaryPath(o.Binary)
	if err != nil {
		return "", false, err
	}

	rendered, err := renderShim(template, binaryPath)
	if err != nil {
		return "", false, err
	}

	path := filepath.Join(dir, shimFilename)
	var changed bool

	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return "", false, fmt.Errorf("shim path %s is a directory", path)
		}
		existing, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", false, fmt.Errorf("read shim %s: %w", path, readErr)
		}
		changed = !bytes.Equal(existing, rendered)
	} else if errors.Is(err, os.ErrNotExist) {
		changed = true
	} else {
		return "", false, fmt.Errorf("stat shim %s: %w", path, err)
	}

	if o.DryRun || !changed {
		return path, changed, nil
	}

	if err := writeShimAtomic(path, rendered, shimFileMode); err != nil {
		return "", false, err
	}

	return path, true, nil
}

func uninstallShim(defaultDir func() (string, error), o ShimOptions) (string, bool, error) {
	dir, err := resolveShimDir(o.Dir, defaultDir)
	if err != nil {
		return "", false, err
	}

	path := filepath.Join(dir, shimFilename)
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return path, false, nil
		}
		return "", false, fmt.Errorf("stat shim %s: %w", path, err)
	}
	if info.IsDir() {
		return "", false, fmt.Errorf("shim path %s is a directory", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("read shim %s: %w", path, err)
	}
	if !bytes.Contains(data, []byte(shimMarker)) {
		return path, false, nil
	}

	if o.DryRun {
		return path, true, nil
	}

	if err := os.Remove(path); err != nil {
		return "", false, fmt.Errorf("remove shim %s: %w", path, err)
	}

	return path, true, nil
}

func resolveShimDir(dir string, defaultDir func() (string, error)) (string, error) {
	if dir != "" {
		return dir, nil
	}
	return defaultDir()
}

func renderShim(template []byte, binaryPath string) ([]byte, error) {
	if !bytes.Contains(template, []byte(shimBinaryPlaceholder)) {
		return nil, errors.New("render shim: template missing binary placeholder")
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(binaryPath); err != nil {
		return nil, fmt.Errorf("quote binary path: %w", err)
	}
	quoted := strings.TrimRight(buf.String(), "\n")

	return bytes.ReplaceAll(template, []byte(shimBinaryPlaceholder), []byte(quoted)), nil
}

func writeShimAtomic(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".donegate.ts.*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp file to %s: %w", path, err)
	}
	cleanup = false
	return nil
}
