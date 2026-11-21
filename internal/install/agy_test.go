package install

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallAgy_FreshInstall(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	hooksPath := filepath.Join(tmp, ".gemini", "config", "hooks.json")

	changed, preview, err := InstallAgy(AgyOptions{
		HooksPath: hooksPath,
		Binary:    "/abs/path/donegate",
	})
	if err != nil {
		t.Fatalf("InstallAgy error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true on fresh install")
	}

	dirInfo, err := os.Stat(filepath.Dir(hooksPath))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("dir mode = %#o, want 0o700", got)
	}

	info, err := os.Stat(hooksPath)
	if err != nil {
		t.Fatalf("stat hooks.json: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %#o, want 0o600", got)
	}

	data, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}
	if !bytes.Equal(data, preview) {
		t.Errorf("written data differs from preview")
	}

	if _, err := os.Stat(hooksPath + ".donegate.bak"); !os.IsNotExist(err) {
		t.Errorf("expected no backup on fresh install, stat err = %v", err)
	}

	doc := mustDecodeMap(t, data)
	if len(doc) != 1 {
		t.Fatalf("expected 1 top-level key, got %d: %v", len(doc), doc)
	}
	dg := mustGetMap(t, doc, "donegate")

	ss := mustGetSlice(t, dg, "SessionStart")
	if len(ss) != 1 {
		t.Fatalf("expected 1 SessionStart entry, got %d", len(ss))
	}
	ssEntry := ss[0].(map[string]any)
	if ssEntry["type"] != "command" || ssEntry["command"] != "/abs/path/donegate session start --format agy" || ssEntry["timeout"] != float64(30) {
		t.Errorf("unexpected SessionStart entry: %#v", ssEntry)
	}

	pre := mustGetSlice(t, dg, "PreToolUse")
	if len(pre) != 1 {
		t.Fatalf("expected 1 PreToolUse group, got %d", len(pre))
	}
	preGroup := pre[0].(map[string]any)
	if preGroup["matcher"] != "*" {
		t.Errorf("PreToolUse matcher = %v, want *", preGroup["matcher"])
	}
	preHooks := mustGetSlice(t, preGroup, "hooks")
	if len(preHooks) != 1 {
		t.Fatalf("expected 1 PreToolUse hook, got %d", len(preHooks))
	}
	preEntry := preHooks[0].(map[string]any)
	if preEntry["type"] != "command" || preEntry["command"] != "/abs/path/donegate guard --format agy" || preEntry["timeout"] != float64(10) {
		t.Errorf("unexpected PreToolUse entry: %#v", preEntry)
	}

	stop := mustGetSlice(t, dg, "Stop")
	if len(stop) != 1 {
		t.Fatalf("expected 1 Stop entry, got %d", len(stop))
	}
	stopEntry := stop[0].(map[string]any)
	if stopEntry["type"] != "command" || stopEntry["command"] != "/abs/path/donegate gate --format agy" || stopEntry["timeout"] != float64(600) {
		t.Errorf("unexpected Stop entry: %#v", stopEntry)
	}
}

func TestInstallAgy_PreservesExistingKeysAndNumbersAndNoHTMLEscape(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	hooksPath := filepath.Join(tmp, "hooks.json")

	original := `{
  "herdr-worker-guard": {
    "PreToolUse": [
      {
        "matcher": "invoke_subagent|manage_subagents|define_subagent|run_workflow",
        "hooks": [
          {
            "type": "command",
            "command": "~/.agents/orchestration/agy-worker-guard.sh && true",
            "timeout": 5
          }
        ]
      }
    ]
  },
  "extraNumber": 42,
  "bigInt": 9007199254740993
}
`
	if err := os.WriteFile(hooksPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, _, err := InstallAgy(AgyOptions{
		HooksPath: hooksPath,
		Binary:    "/usr/local/bin/donegate",
	})
	if err != nil {
		t.Fatalf("InstallAgy error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}

	info, err := os.Stat(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("file mode = %#o, want 0o644", got)
	}

	data, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "agy-worker-guard.sh && true") {
		t.Errorf("expected no HTML escaping, got:\n%s", string(data))
	}

	if !strings.Contains(string(data), `"bigInt": 9007199254740993`) {
		t.Errorf("expected large integer preserved intact, got:\n%s", string(data))
	}

	doc := mustDecodeMap(t, data)
	if doc["extraNumber"] != float64(42) {
		t.Errorf("extraNumber = %#v (%T), want 42", doc["extraNumber"], doc["extraNumber"])
	}
	hwg := mustGetMap(t, doc, "herdr-worker-guard")
	pre := mustGetSlice(t, hwg, "PreToolUse")
	hookEntry := pre[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	if hookEntry["timeout"] != float64(5) {
		t.Errorf("timeout = %#v, want 5", hookEntry["timeout"])
	}

	bak, err := os.ReadFile(hooksPath + ".donegate.bak")
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(bak) != original {
		t.Errorf("backup mismatch:\ngot:\n%s\nwant:\n%s", string(bak), original)
	}
}

func TestInstallAgy_IdempotentAndBackupOnceAndSpacesAndDryRun(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	hooksPath := filepath.Join(tmp, "hooks.json")

	original := "{\n  \"herdr-worker-guard\": {}\n}\n"
	if err := os.WriteFile(hooksPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	opts := AgyOptions{
		HooksPath: hooksPath,
		Binary:    "/path with spaces/donegate",
	}
	changed1, _, err := InstallAgy(opts)
	if err != nil || !changed1 {
		t.Fatalf("first install: changed=%v err=%v", changed1, err)
	}

	data, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "'/path with spaces/donegate' guard --format agy") {
		t.Errorf("expected quoted binary path in hooks.json:\n%s", string(data))
	}

	// Idempotent second install: changed=false and file untouched.
	changed2, _, err := InstallAgy(opts)
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if changed2 {
		t.Errorf("expected changed=false on idempotent second install")
	}
	after, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, after) {
		t.Errorf("file modified on idempotent install")
	}

	// Reinstall with a new binary updates donegate key, keeps original backup intact.
	changed3, _, err := InstallAgy(AgyOptions{
		HooksPath: hooksPath,
		Binary:    "/v2/donegate",
	})
	if err != nil || !changed3 {
		t.Fatalf("reinstall: changed=%v err=%v", changed3, err)
	}
	bak, err := os.ReadFile(hooksPath + ".donegate.bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(bak) != original {
		t.Errorf("backup overwritten: got %q, want %q", string(bak), original)
	}

	// DryRun does not modify file.
	dryPath := filepath.Join(tmp, "dry", "hooks.json")
	changedDry, preview, err := InstallAgy(AgyOptions{
		HooksPath: dryPath,
		Binary:    "/v2/donegate",
		DryRun:    true,
	})
	if err != nil || !changedDry {
		t.Fatalf("dry-run: changed=%v err=%v", changedDry, err)
	}
	if len(preview) == 0 {
		t.Errorf("expected non-empty dry-run preview")
	}
	if _, err := os.Stat(dryPath); !os.IsNotExist(err) {
		t.Errorf("dry-run should not write file, stat err = %v", err)
	}
}

func TestInstallAgy_InvalidJSON(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
	}{
		{name: "syntax error", content: "{bad"},
		{name: "null", content: "null\n"},
		{name: "array", content: "[]\n"},
		{name: "empty", content: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("HOME", tmp)
			hooksPath := filepath.Join(tmp, "hooks.json")
			if err := os.WriteFile(hooksPath, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}

			changed, _, err := InstallAgy(AgyOptions{
				HooksPath: hooksPath,
				Binary:    "/x/donegate",
			})
			if err == nil {
				t.Fatalf("expected error for invalid JSON %q", tc.content)
			}
			if changed {
				t.Errorf("expected changed=false on error")
			}
			got, _ := os.ReadFile(hooksPath)
			if string(got) != tc.content {
				t.Errorf("file modified on error")
			}
		})
	}
}

func TestUninstallAgy(t *testing.T) {
	t.Run("removes only donegate key and leaves other hooks intact", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		hooksPath := filepath.Join(tmp, "hooks.json")

		original := `{
  "herdr-worker-guard": {
    "PreToolUse": []
  }
}
`
		if err := os.WriteFile(hooksPath, []byte(original), 0o600); err != nil {
			t.Fatal(err)
		}

		if _, _, err := InstallAgy(AgyOptions{HooksPath: hooksPath, Binary: "/x/donegate"}); err != nil {
			t.Fatal(err)
		}

		changed, err := UninstallAgy(AgyOptions{HooksPath: hooksPath})
		if err != nil {
			t.Fatalf("UninstallAgy error: %v", err)
		}
		if !changed {
			t.Fatalf("expected changed=true on uninstall")
		}

		data, err := os.ReadFile(hooksPath)
		if err != nil {
			t.Fatal(err)
		}
		doc := mustDecodeMap(t, data)
		if _, exists := doc["donegate"]; exists {
			t.Errorf("donegate key still present: %v", doc)
		}
		if _, exists := doc["herdr-worker-guard"]; !exists {
			t.Errorf("herdr-worker-guard key was removed: %v", doc)
		}

		// Second uninstall is a no-op.
		changed2, err := UninstallAgy(AgyOptions{HooksPath: hooksPath})
		if err != nil {
			t.Fatalf("second UninstallAgy error: %v", err)
		}
		if changed2 {
			t.Errorf("expected changed=false on second uninstall")
		}
	})

	t.Run("missing file is a no-op", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		changed, err := UninstallAgy(AgyOptions{HooksPath: filepath.Join(tmp, "missing.json")})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if changed {
			t.Errorf("expected changed=false")
		}
	})
}
