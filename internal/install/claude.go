package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

const preToolUseMatcher = "Edit|Write|MultiEdit|NotebookEdit|Bash"

var donegateCommandSubstrings = []string{
	"guard --format claude",
	"gate --format claude",
	"session start --format claude",
}

// ClaudeOptions configures installing or uninstalling donegate hooks in Claude Code settings.
type ClaudeOptions struct {
	SettingsPath string // default <home>/.claude/settings.json
	Binary       string // absolute path to the donegate binary; default os.Executable() resolved with filepath.EvalSymlinks
	SubagentStop bool   // default true (include SubagentStop hook when true)
	DryRun       bool
}

// DefaultClaudeSettingsPath returns the default user-level Claude Code settings path.
func DefaultClaudeSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// InstallClaude idempotently merges donegate hooks into the Claude Code settings JSON file.
func InstallClaude(opts ClaudeOptions) (changed bool, preview []byte, err error) {
	settingsPath, err := resolveSettingsPath(opts.SettingsPath)
	if err != nil {
		return false, nil, err
	}

	binaryPath, err := resolveBinaryPath(opts.Binary)
	if err != nil {
		return false, nil, err
	}

	existed, origBytes, origMode, err := readSettingsFile(settingsPath)
	if err != nil {
		return false, nil, err
	}

	var origDoc map[string]any
	doc := make(map[string]any)
	if existed {
		origDoc, err = decodeJSONObject(origBytes)
		if err != nil {
			return false, nil, fmt.Errorf("parse settings %s: %w", settingsPath, err)
		}
		doc, err = decodeJSONObject(origBytes)
		if err != nil {
			return false, nil, fmt.Errorf("parse settings %s: %w", settingsPath, err)
		}
	}

	hooksMap, err := getOrCreateHooksMap(doc)
	if err != nil {
		return false, nil, fmt.Errorf("parse settings %s: %w", settingsPath, err)
	}

	removeDonegateHooks(hooksMap)
	appendDonegateHooks(hooksMap, formatBinaryCommand(binaryPath), opts.SubagentStop)
	doc["hooks"] = hooksMap

	outBytes, err := encodeJSONObject(doc)
	if err != nil {
		return false, nil, fmt.Errorf("encode settings: %w", err)
	}

	if existed {
		afterDoc, err := decodeJSONObject(outBytes)
		if err != nil {
			return false, nil, fmt.Errorf("verify encoded settings: %w", err)
		}
		changed = !reflect.DeepEqual(origDoc, afterDoc)
	} else {
		changed = true
	}

	if opts.DryRun || !changed {
		return changed, outBytes, nil
	}

	if existed {
		if err := createBackupOnce(settingsPath, origBytes, origMode); err != nil {
			return false, nil, err
		}
	}

	if err := writeFileAtomic(settingsPath, outBytes, origMode); err != nil {
		return false, nil, err
	}

	return true, outBytes, nil
}

// UninstallClaude removes donegate hooks from the Claude Code settings JSON file.
func UninstallClaude(opts ClaudeOptions) (changed bool, err error) {
	settingsPath, err := resolveSettingsPath(opts.SettingsPath)
	if err != nil {
		return false, err
	}

	existed, origBytes, origMode, err := readSettingsFile(settingsPath)
	if err != nil {
		return false, err
	}
	if !existed {
		return false, nil
	}

	origDoc, err := decodeJSONObject(origBytes)
	if err != nil {
		return false, fmt.Errorf("parse settings %s: %w", settingsPath, err)
	}
	doc, err := decodeJSONObject(origBytes)
	if err != nil {
		return false, fmt.Errorf("parse settings %s: %w", settingsPath, err)
	}

	rawHooks, hasHooks := doc["hooks"]
	if !hasHooks || rawHooks == nil {
		return false, nil
	}
	hooksMap, ok := rawHooks.(map[string]any)
	if !ok {
		return false, fmt.Errorf("parse settings %s: invalid \"hooks\" field: expected object", settingsPath)
	}

	removedAny := removeDonegateHooks(hooksMap)
	if !removedAny {
		return false, nil
	}

	if len(hooksMap) == 0 {
		delete(doc, "hooks")
	}

	outBytes, err := encodeJSONObject(doc)
	if err != nil {
		return false, fmt.Errorf("encode settings: %w", err)
	}

	afterDoc, err := decodeJSONObject(outBytes)
	if err != nil {
		return false, fmt.Errorf("verify encoded settings: %w", err)
	}
	if reflect.DeepEqual(origDoc, afterDoc) {
		return false, nil
	}

	if err := writeFileAtomic(settingsPath, outBytes, origMode); err != nil {
		return false, err
	}

	return true, nil
}

func resolveSettingsPath(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	return DefaultClaudeSettingsPath()
}

func resolveBinaryPath(binary string) (string, error) {
	if binary == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("resolve executable: %w", err)
		}
		resolved, err := filepath.EvalSymlinks(exe)
		if err != nil {
			return "", fmt.Errorf("resolve executable symlinks: %w", err)
		}
		return resolved, nil
	}
	if !filepath.IsAbs(binary) {
		abs, err := filepath.Abs(binary)
		if err != nil {
			return "", fmt.Errorf("resolve binary path: %w", err)
		}
		return abs, nil
	}
	return binary, nil
}

func formatBinaryCommand(binary string) string {
	if strings.ContainsAny(binary, " \t") {
		return "'" + strings.ReplaceAll(binary, "'", `'\''`) + "'"
	}
	return binary
}

func isDonegateCommand(cmd string) bool {
	for _, sub := range donegateCommandSubstrings {
		if strings.Contains(cmd, "donegate "+sub) ||
			strings.Contains(cmd, "donegate' "+sub) ||
			strings.Contains(cmd, `donegate" `+sub) {
			return true
		}
	}
	return false
}

func getOrCreateHooksMap(doc map[string]any) (map[string]any, error) {
	rawHooks, exists := doc["hooks"]
	if !exists || rawHooks == nil {
		return make(map[string]any), nil
	}
	hooksMap, ok := rawHooks.(map[string]any)
	if !ok {
		return nil, errors.New(`invalid "hooks" field: expected object`)
	}
	return hooksMap, nil
}

func removeDonegateHooks(hooksMap map[string]any) bool {
	removedAny := false

	for eventName, rawGroups := range hooksMap {
		groups, ok := rawGroups.([]any)
		if !ok {
			continue
		}

		keptGroups := make([]any, 0, len(groups))
		eventChanged := false

		for _, rawGroup := range groups {
			groupMap, ok := rawGroup.(map[string]any)
			if !ok {
				keptGroups = append(keptGroups, rawGroup)
				continue
			}

			rawEntries, ok := groupMap["hooks"].([]any)
			if !ok {
				keptGroups = append(keptGroups, rawGroup)
				continue
			}

			keptEntries := make([]any, 0, len(rawEntries))
			groupChanged := false
			for _, rawEntry := range rawEntries {
				entryMap, ok := rawEntry.(map[string]any)
				if ok {
					if cmd, ok := entryMap["command"].(string); ok && isDonegateCommand(cmd) {
						groupChanged = true
						removedAny = true
						continue
					}
				}
				keptEntries = append(keptEntries, rawEntry)
			}

			if groupChanged {
				eventChanged = true
				if len(keptEntries) == 0 {
					continue
				}
				groupMap["hooks"] = keptEntries
			}
			keptGroups = append(keptGroups, groupMap)
		}

		if eventChanged {
			if len(keptGroups) == 0 {
				delete(hooksMap, eventName)
			} else {
				hooksMap[eventName] = keptGroups
			}
		}
	}

	return removedAny
}

func appendDonegateHooks(hooksMap map[string]any, binCmd string, subagentStop bool) {
	appendEventGroup(hooksMap, "PreToolUse", map[string]any{
		"matcher": preToolUseMatcher,
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": binCmd + " guard --format claude",
				"timeout": 10,
			},
		},
	})

	appendEventGroup(hooksMap, "Stop", map[string]any{
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": binCmd + " gate --format claude",
				"timeout": 600,
			},
		},
	})

	if subagentStop {
		appendEventGroup(hooksMap, "SubagentStop", map[string]any{
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": binCmd + " gate --format claude",
					"timeout": 600,
				},
			},
		})
	}

	appendEventGroup(hooksMap, "SessionStart", map[string]any{
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": binCmd + " session start --format claude",
				"timeout": 30,
			},
		},
	})
}

func appendEventGroup(hooksMap map[string]any, eventName string, group map[string]any) {
	var groups []any
	if existing, ok := hooksMap[eventName].([]any); ok {
		groups = existing
	}
	hooksMap[eventName] = append(groups, group)
}

func readSettingsFile(path string) (existed bool, data []byte, mode fs.FileMode, err error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil, 0o600, nil
		}
		return false, nil, 0, fmt.Errorf("stat settings %s: %w", path, err)
	}
	if info.IsDir() {
		return false, nil, 0, fmt.Errorf("settings path %s is a directory", path)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		return false, nil, 0, fmt.Errorf("read settings %s: %w", path, err)
	}
	return true, data, info.Mode().Perm(), nil
}

func decodeJSONObject(data []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, errors.New("unexpected trailing data in JSON")
	}
	if obj == nil {
		return nil, errors.New("expected JSON object, got null")
	}
	return obj, nil
}

func encodeJSONObject(doc map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func createBackupOnce(settingsPath string, origBytes []byte, mode fs.FileMode) error {
	backupPath := settingsPath + ".donegate.bak"
	if _, err := os.Stat(backupPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat backup %s: %w", backupPath, err)
	}

	f, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return fmt.Errorf("create backup %s: %w", backupPath, err)
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		_ = os.Remove(backupPath)
		return fmt.Errorf("chmod backup %s: %w", backupPath, err)
	}
	if _, err := f.Write(origBytes); err != nil {
		_ = f.Close()
		_ = os.Remove(backupPath)
		return fmt.Errorf("write backup %s: %w", backupPath, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(backupPath)
		return fmt.Errorf("close backup %s: %w", backupPath, err)
	}
	return nil
}

func writeFileAtomic(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".settings.json.donegate.*")
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
