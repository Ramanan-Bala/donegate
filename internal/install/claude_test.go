package install

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallClaude_FreshInstall(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	settingsPath := filepath.Join(tmp, ".claude", "settings.json")

	changed, preview, err := InstallClaude(ClaudeOptions{
		SettingsPath: settingsPath,
		Binary:       "/abs/path/donegate",
		SubagentStop: true,
	})
	if err != nil {
		t.Fatalf("InstallClaude error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true on fresh install")
	}

	dirInfo, err := os.Stat(filepath.Dir(settingsPath))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("dir mode = %#o, want 0o700", got)
	}

	info, err := os.Stat(settingsPath)
	if err != nil {
		t.Fatalf("stat settings: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %#o, want 0o600", got)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if !bytes.Equal(data, preview) {
		t.Errorf("written data differs from preview")
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Errorf("expected trailing newline")
	}
	if !strings.Contains(string(data), "\n  \"hooks\": {\n") {
		t.Errorf("expected 2-space indentation, got:\n%s", string(data))
	}

	// No backup should exist for a fresh install when no original file existed.
	if _, err := os.Stat(settingsPath + ".donegate.bak"); !os.IsNotExist(err) {
		t.Errorf("expected no backup on fresh install, stat err = %v", err)
	}

	doc := mustDecodeMap(t, data)
	hooks := mustGetMap(t, doc, "hooks")
	if len(hooks) != 4 {
		t.Fatalf("expected 4 hook events, got %d: %v", len(hooks), hooks)
	}

	assertSingleHook(t, hooks, "PreToolUse", "Edit|Write|MultiEdit|NotebookEdit|Bash", "/abs/path/donegate guard --format claude", 10)
	assertSingleHook(t, hooks, "Stop", "", "/abs/path/donegate gate --format claude", 600)
	assertSingleHook(t, hooks, "SubagentStop", "", "/abs/path/donegate gate --format claude", 600)
	assertSingleHook(t, hooks, "SessionStart", "", "/abs/path/donegate session start --format claude", 30)
}

func TestInstallClaude_PreservesExistingAndUserHook(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	settingsPath := filepath.Join(tmp, "settings.json")

	original := `{
  "model": "claude-opus-4",
  "permissions": {
    "allow": ["Read", "Grep"]
  },
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "echo check && true"
          }
        ]
      }
    ]
  }
}
`
	if err := os.WriteFile(settingsPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, _, err := InstallClaude(ClaudeOptions{
		SettingsPath: settingsPath,
		Binary:       "/usr/local/bin/donegate",
		SubagentStop: true,
	})
	if err != nil {
		t.Fatalf("InstallClaude error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}

	// File mode should be preserved.
	info, err := os.Stat(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("file mode = %#o, want 0o644", got)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	// Ensure HTML characters like && are not escaped.
	if !strings.Contains(string(data), "echo check && true") {
		t.Errorf("user command should not be HTML-escaped, got:\n%s", string(data))
	}

	doc := mustDecodeMap(t, data)
	if doc["model"] != "claude-opus-4" {
		t.Errorf("model = %v, want claude-opus-4", doc["model"])
	}
	perms := mustGetMap(t, doc, "permissions")
	allow, ok := perms["allow"].([]any)
	if !ok || len(allow) != 2 || allow[0] != "Read" || allow[1] != "Grep" {
		t.Errorf("permissions.allow corrupted: %#v", perms["allow"])
	}

	hooks := mustGetMap(t, doc, "hooks")
	preToolUse := mustGetSlice(t, hooks, "PreToolUse")
	if len(preToolUse) != 2 {
		t.Fatalf("expected 2 PreToolUse groups (user + donegate), got %d", len(preToolUse))
	}
	userGroup := preToolUse[0].(map[string]any)
	if userGroup["matcher"] != "Bash" {
		t.Errorf("user PreToolUse matcher = %v, want Bash", userGroup["matcher"])
	}
	donegateGroup := preToolUse[1].(map[string]any)
	if donegateGroup["matcher"] != preToolUseMatcher {
		t.Errorf("donegate PreToolUse matcher = %v, want %s", donegateGroup["matcher"], preToolUseMatcher)
	}

	// Backup should contain exact original content.
	bak, err := os.ReadFile(settingsPath + ".donegate.bak")
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(bak) != original {
		t.Errorf("backup content mismatch:\ngot:\n%s\nwant:\n%s", string(bak), original)
	}
}

func TestInstallClaude_IdempotentSecondInstall(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	settingsPath := filepath.Join(tmp, "settings.json")

	opts := ClaudeOptions{
		SettingsPath: settingsPath,
		Binary:       "/usr/local/bin/donegate",
		SubagentStop: true,
	}

	changed1, _, err := InstallClaude(opts)
	if err != nil {
		t.Fatalf("first install: %v", err)
	}
	if !changed1 {
		t.Fatalf("expected first install changed=true")
	}

	before, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}

	changed2, _, err := InstallClaude(opts)
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if changed2 {
		t.Errorf("expected second install changed=false")
	}

	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("file changed on idempotent second install")
	}
}

func TestInstallClaude_ReinstallDifferentBinaryAndBackupOnce(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	settingsPath := filepath.Join(tmp, "settings.json")

	original := "{\"model\": \"claude-sonnet-4\"}\n"
	if err := os.WriteFile(settingsPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	changed1, _, err := InstallClaude(ClaudeOptions{
		SettingsPath: settingsPath,
		Binary:       "/v1/donegate",
		SubagentStop: true,
	})
	if err != nil || !changed1 {
		t.Fatalf("first install: changed=%v err=%v", changed1, err)
	}

	changed2, _, err := InstallClaude(ClaudeOptions{
		SettingsPath: settingsPath,
		Binary:       "/v2/donegate",
		SubagentStop: true,
	})
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if !changed2 {
		t.Fatalf("expected changed=true when binary path changes")
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "/v1/donegate") {
		t.Errorf("old binary path still present:\n%s", string(data))
	}

	doc := mustDecodeMap(t, data)
	hooks := mustGetMap(t, doc, "hooks")
	assertSingleHook(t, hooks, "PreToolUse", preToolUseMatcher, "/v2/donegate guard --format claude", 10)
	assertSingleHook(t, hooks, "Stop", "", "/v2/donegate gate --format claude", 600)
	assertSingleHook(t, hooks, "SubagentStop", "", "/v2/donegate gate --format claude", 600)
	assertSingleHook(t, hooks, "SessionStart", "", "/v2/donegate session start --format claude", 30)

	// Backup must still contain the original pre-donegate content (created once).
	bak, err := os.ReadFile(settingsPath + ".donegate.bak")
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(bak) != original {
		t.Errorf("backup was overwritten: got %q, want %q", string(bak), original)
	}
}

func TestInstallClaude_InvalidJSON(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "syntax error", content: "{not valid json"},
		{name: "null root", content: "null\n"},
		{name: "array root", content: "[]\n"},
		{name: "empty file", content: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("HOME", tmp)
			settingsPath := filepath.Join(tmp, "settings.json")
			if err := os.WriteFile(settingsPath, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}

			changed, _, err := InstallClaude(ClaudeOptions{
				SettingsPath: settingsPath,
				Binary:       "/x/donegate",
				SubagentStop: true,
			})
			if err == nil {
				t.Fatalf("expected error for invalid JSON %q, got nil", tc.content)
			}
			if changed {
				t.Errorf("expected changed=false on error")
			}

			got, readErr := os.ReadFile(settingsPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != tc.content {
				t.Errorf("file modified on invalid JSON: got %q, want %q", string(got), tc.content)
			}
			if _, statErr := os.Stat(settingsPath + ".donegate.bak"); !os.IsNotExist(statErr) {
				t.Errorf("backup should not be created on invalid JSON")
			}
		})
	}
}

func TestInstallClaude_NoSubagent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	settingsPath := filepath.Join(tmp, "settings.json")

	// First install without SubagentStop.
	changed, _, err := InstallClaude(ClaudeOptions{
		SettingsPath: settingsPath,
		Binary:       "/x/donegate",
		SubagentStop: false,
	})
	if err != nil || !changed {
		t.Fatalf("install --no-subagent: changed=%v err=%v", changed, err)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	doc := mustDecodeMap(t, data)
	hooks := mustGetMap(t, doc, "hooks")
	if _, exists := hooks["SubagentStop"]; exists {
		t.Errorf("expected SubagentStop to be omitted, got %v", hooks["SubagentStop"])
	}
	if len(hooks) != 3 {
		t.Errorf("expected 3 hook events, got %d", len(hooks))
	}

	// Installing with SubagentStop=true adds it; reinstalling with false removes it.
	if _, _, err := InstallClaude(ClaudeOptions{
		SettingsPath: settingsPath,
		Binary:       "/x/donegate",
		SubagentStop: true,
	}); err != nil {
		t.Fatal(err)
	}
	changed, _, err = InstallClaude(ClaudeOptions{
		SettingsPath: settingsPath,
		Binary:       "/x/donegate",
		SubagentStop: false,
	})
	if err != nil || !changed {
		t.Fatalf("reinstall without subagent: changed=%v err=%v", changed, err)
	}
	data, err = os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	doc = mustDecodeMap(t, data)
	hooks = mustGetMap(t, doc, "hooks")
	if _, exists := hooks["SubagentStop"]; exists {
		t.Errorf("expected SubagentStop to be removed on reinstall with SubagentStop=false")
	}
}

func TestUninstallClaude(t *testing.T) {
	t.Run("removes only donegate entries and leaves user hook", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		settingsPath := filepath.Join(tmp, "settings.json")

		initial := `{
  "model": "claude-opus-4",
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "/usr/local/bin/user-guard"
          }
        ]
      }
    ]
  }
}
`
		if err := os.WriteFile(settingsPath, []byte(initial), 0o600); err != nil {
			t.Fatal(err)
		}

		if _, _, err := InstallClaude(ClaudeOptions{
			SettingsPath: settingsPath,
			Binary:       "/x/donegate",
			SubagentStop: true,
		}); err != nil {
			t.Fatal(err)
		}

		changed, err := UninstallClaude(ClaudeOptions{SettingsPath: settingsPath})
		if err != nil {
			t.Fatalf("UninstallClaude error: %v", err)
		}
		if !changed {
			t.Fatalf("expected changed=true on uninstall")
		}

		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatal(err)
		}
		doc := mustDecodeMap(t, data)
		if doc["model"] != "claude-opus-4" {
			t.Errorf("model lost: %v", doc["model"])
		}
		hooks := mustGetMap(t, doc, "hooks")
		if len(hooks) != 1 {
			t.Fatalf("expected only PreToolUse remaining, got %v", hooks)
		}
		preToolUse := mustGetSlice(t, hooks, "PreToolUse")
		if len(preToolUse) != 1 {
			t.Fatalf("expected 1 user PreToolUse group, got %d", len(preToolUse))
		}
		group := preToolUse[0].(map[string]any)
		entries := mustGetSlice(t, group, "hooks")
		if len(entries) != 1 || entries[0].(map[string]any)["command"] != "/usr/local/bin/user-guard" {
			t.Errorf("user hook corrupted: %#v", entries)
		}

		// Second uninstall should be a no-op.
		changed2, err := UninstallClaude(ClaudeOptions{SettingsPath: settingsPath})
		if err != nil {
			t.Fatalf("second uninstall error: %v", err)
		}
		if changed2 {
			t.Errorf("expected changed=false on second uninstall")
		}
	})

	t.Run("drops empty hooks object", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		settingsPath := filepath.Join(tmp, "settings.json")

		if _, _, err := InstallClaude(ClaudeOptions{
			SettingsPath: settingsPath,
			Binary:       "/x/donegate",
			SubagentStop: true,
		}); err != nil {
			t.Fatal(err)
		}

		changed, err := UninstallClaude(ClaudeOptions{SettingsPath: settingsPath})
		if err != nil {
			t.Fatalf("uninstall error: %v", err)
		}
		if !changed {
			t.Fatalf("expected changed=true")
		}

		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatal(err)
		}
		doc := mustDecodeMap(t, data)
		if _, exists := doc["hooks"]; exists {
			t.Errorf("expected empty hooks object to be dropped, got %v", doc["hooks"])
		}
	})

	t.Run("missing file returns changed=false nil", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		settingsPath := filepath.Join(tmp, "does-not-exist", "settings.json")

		changed, err := UninstallClaude(ClaudeOptions{SettingsPath: settingsPath})
		if err != nil {
			t.Fatalf("expected nil error for missing file, got %v", err)
		}
		if changed {
			t.Errorf("expected changed=false for missing file")
		}
		if _, statErr := os.Stat(settingsPath); !os.IsNotExist(statErr) {
			t.Errorf("missing file should not be created")
		}
	})
}

func TestInstallClaude_DryRunWritesNothing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	settingsPath := filepath.Join(tmp, ".claude", "settings.json")

	changed, preview, err := InstallClaude(ClaudeOptions{
		SettingsPath: settingsPath,
		Binary:       "/x/donegate",
		SubagentStop: true,
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("dry-run error: %v", err)
	}
	if !changed {
		t.Errorf("expected changed=true for dry-run on missing file")
	}
	if len(preview) == 0 {
		t.Fatalf("expected non-empty preview")
	}
	doc := mustDecodeMap(t, preview)
	hooks := mustGetMap(t, doc, "hooks")
	assertSingleHook(t, hooks, "PreToolUse", preToolUseMatcher, "/x/donegate guard --format claude", 10)

	if _, err := os.Stat(filepath.Dir(settingsPath)); !os.IsNotExist(err) {
		t.Errorf("dry-run should not create directory, stat err = %v", err)
	}
	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		t.Errorf("dry-run should not create file, stat err = %v", err)
	}
}

func TestInstallClaude_BinaryPathWithSpaces(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	settingsPath := filepath.Join(tmp, "settings.json")
	spacedBinary := "/Users/alice/My Tools/donegate"

	changed, _, err := InstallClaude(ClaudeOptions{
		SettingsPath: settingsPath,
		Binary:       spacedBinary,
		SubagentStop: true,
	})
	if err != nil || !changed {
		t.Fatalf("install with spaces: changed=%v err=%v", changed, err)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	doc := mustDecodeMap(t, data)
	hooks := mustGetMap(t, doc, "hooks")

	assertSingleHook(t, hooks, "PreToolUse", preToolUseMatcher, "'/Users/alice/My Tools/donegate' guard --format claude", 10)
	assertSingleHook(t, hooks, "Stop", "", "'/Users/alice/My Tools/donegate' gate --format claude", 600)
	assertSingleHook(t, hooks, "SubagentStop", "", "'/Users/alice/My Tools/donegate' gate --format claude", 600)
	assertSingleHook(t, hooks, "SessionStart", "", "'/Users/alice/My Tools/donegate' session start --format claude", 30)

	// Re-installing with the same spaced path must be idempotent.
	changed2, _, err := InstallClaude(ClaudeOptions{
		SettingsPath: settingsPath,
		Binary:       spacedBinary,
		SubagentStop: true,
	})
	if err != nil {
		t.Fatalf("second install with spaces: %v", err)
	}
	if changed2 {
		t.Errorf("expected second install with spaced binary to be idempotent (changed=false)")
	}

	// Uninstalling must recognize and remove the quoted-path hooks.
	uninstalled, err := UninstallClaude(ClaudeOptions{SettingsPath: settingsPath})
	if err != nil {
		t.Fatalf("uninstall with spaces: %v", err)
	}
	if !uninstalled {
		t.Errorf("expected uninstall to remove hooks with spaced binary path")
	}
	afterUninstall, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	afterDoc := mustDecodeMap(t, afterUninstall)
	if _, exists := afterDoc["hooks"]; exists {
		t.Errorf("expected hooks to be removed after uninstall, got %v", afterDoc["hooks"])
	}
}

func mustDecodeMap(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal JSON: %v\ncontent:\n%s", err, string(data))
	}
	return m
}

func mustGetMap(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("expected object at key %q, got %T (%#v)", key, m[key], m[key])
	}
	return v
}

func mustGetSlice(t *testing.T, m map[string]any, key string) []any {
	t.Helper()
	v, ok := m[key].([]any)
	if !ok {
		t.Fatalf("expected slice at key %q, got %T (%#v)", key, m[key], m[key])
	}
	return v
}

func assertSingleHook(t *testing.T, hooks map[string]any, event, wantMatcher, wantCommand string, wantTimeout float64) {
	t.Helper()
	groups := mustGetSlice(t, hooks, event)
	if len(groups) != 1 {
		t.Fatalf("%s: expected 1 group, got %d (%#v)", event, len(groups), groups)
	}
	group, ok := groups[0].(map[string]any)
	if !ok {
		t.Fatalf("%s: expected map group, got %T", event, groups[0])
	}
	if wantMatcher == "" {
		if _, exists := group["matcher"]; exists {
			t.Errorf("%s: expected no matcher field, got %v", event, group["matcher"])
		}
	} else {
		if got := group["matcher"]; got != wantMatcher {
			t.Errorf("%s: matcher = %v, want %q", event, got, wantMatcher)
		}
	}
	entries := mustGetSlice(t, group, "hooks")
	if len(entries) != 1 {
		t.Fatalf("%s: expected 1 hook entry, got %d", event, len(entries))
	}
	entry, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatalf("%s: expected map entry, got %T", event, entries[0])
	}
	if entry["type"] != "command" {
		t.Errorf("%s: type = %v, want command", event, entry["type"])
	}
	if entry["command"] != wantCommand {
		t.Errorf("%s: command = %v, want %q", event, entry["command"], wantCommand)
	}
	if entry["timeout"] != wantTimeout {
		t.Errorf("%s: timeout = %v, want %v", event, entry["timeout"], wantTimeout)
	}
}
