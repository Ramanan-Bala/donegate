package install

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

// AgyOptions configures installing or uninstalling donegate hooks in Antigravity's hooks.json.
type AgyOptions struct {
	HooksPath string // default ~/.gemini/config/hooks.json; with --dir <d> it is <d>/hooks.json
	Binary    string // absolute path to the donegate binary; default os.Executable() resolved with filepath.EvalSymlinks
	DryRun    bool
}

// DefaultAgyHooksPath returns the default user-level Antigravity hooks.json path.
func DefaultAgyHooksPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".gemini", "config", "hooks.json"), nil
}

// InstallAgy idempotently merges the top-level "donegate" hook entry into Antigravity's hooks.json.
func InstallAgy(o AgyOptions) (changed bool, preview []byte, err error) {
	hooksPath, err := resolveAgyHooksPath(o.HooksPath)
	if err != nil {
		return false, nil, err
	}

	binaryPath, err := resolveBinaryPath(o.Binary)
	if err != nil {
		return false, nil, err
	}

	existed, origBytes, origMode, err := readSettingsFile(hooksPath)
	if err != nil {
		return false, nil, err
	}

	var origDoc map[string]any
	doc := make(map[string]any)
	if existed {
		origDoc, err = decodeJSONObject(origBytes)
		if err != nil {
			return false, nil, fmt.Errorf("parse hooks %s: %w", hooksPath, err)
		}
		doc, err = decodeJSONObject(origBytes)
		if err != nil {
			return false, nil, fmt.Errorf("parse hooks %s: %w", hooksPath, err)
		}
	}

	doc["donegate"] = buildAgyDonegateHook(formatBinaryCommand(binaryPath))

	outBytes, err := encodeJSONObject(doc)
	if err != nil {
		return false, nil, fmt.Errorf("encode hooks: %w", err)
	}

	if existed {
		afterDoc, err := decodeJSONObject(outBytes)
		if err != nil {
			return false, nil, fmt.Errorf("verify encoded hooks: %w", err)
		}
		changed = !reflect.DeepEqual(origDoc, afterDoc)
	} else {
		changed = true
	}

	if o.DryRun || !changed {
		return changed, outBytes, nil
	}

	if existed {
		if err := createBackupOnce(hooksPath, origBytes, origMode); err != nil {
			return false, nil, err
		}
	}

	if err := writeFileAtomic(hooksPath, outBytes, origMode); err != nil {
		return false, nil, err
	}

	return true, outBytes, nil
}

// UninstallAgy removes only the top-level "donegate" key from Antigravity's hooks.json.
func UninstallAgy(o AgyOptions) (changed bool, err error) {
	hooksPath, err := resolveAgyHooksPath(o.HooksPath)
	if err != nil {
		return false, err
	}

	existed, origBytes, origMode, err := readSettingsFile(hooksPath)
	if err != nil {
		return false, err
	}
	if !existed {
		return false, nil
	}

	doc, err := decodeJSONObject(origBytes)
	if err != nil {
		return false, fmt.Errorf("parse hooks %s: %w", hooksPath, err)
	}

	if _, exists := doc["donegate"]; !exists {
		return false, nil
	}

	if o.DryRun {
		return true, nil
	}

	delete(doc, "donegate")

	outBytes, err := encodeJSONObject(doc)
	if err != nil {
		return false, fmt.Errorf("encode hooks: %w", err)
	}

	if err := writeFileAtomic(hooksPath, outBytes, origMode); err != nil {
		return false, err
	}

	return true, nil
}

func resolveAgyHooksPath(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	return DefaultAgyHooksPath()
}

func buildAgyDonegateHook(binCmd string) map[string]any {
	return map[string]any{
		"SessionStart": []any{
			map[string]any{
				"type":    "command",
				"command": binCmd + " session start --format agy",
				"timeout": 30,
			},
		},
		"PreToolUse": []any{
			map[string]any{
				"matcher": "*",
				"hooks": []any{
					map[string]any{
						"type":    "command",
						"command": binCmd + " guard --format agy",
						"timeout": 10,
					},
				},
			},
		},
		"Stop": []any{
			map[string]any{
				"type":    "command",
				"command": binCmd + " gate --format agy",
				"timeout": 600,
			},
		},
	}
}
