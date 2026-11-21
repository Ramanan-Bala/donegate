package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newTestInstallRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "donegate",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newInstallCmd())
	root.AddCommand(newUninstallCmd())
	return root
}

func TestInstallAndUninstallCmd(t *testing.T) {
	t.Run("install claude then idempotent reinstall then uninstall", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		settingsPath := filepath.Join(tmp, ".claude", "settings.json")

		// Fresh install.
		root := newTestInstallRoot()
		var stdout, stderr bytes.Buffer
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		root.SetArgs([]string{"install", "claude", "--settings", settingsPath, "--binary", "/x/donegate"})

		if err := root.Execute(); err != nil {
			t.Fatalf("install failed: %v (stderr=%s)", err, stderr.String())
		}
		wantInstalled := "donegate: installed Claude Code hooks in " + settingsPath
		if !strings.Contains(stdout.String(), wantInstalled) {
			t.Errorf("stdout = %q, want substring %q", stdout.String(), wantInstalled)
		}

		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("read settings: %v", err)
		}
		if !strings.Contains(string(data), "/x/donegate guard --format claude") {
			t.Errorf("expected guard hook in settings:\n%s", string(data))
		}
		if !strings.Contains(string(data), `"SubagentStop"`) {
			t.Errorf("expected SubagentStop hook by default:\n%s", string(data))
		}

		// Idempotent second install.
		root2 := newTestInstallRoot()
		stdout.Reset()
		stderr.Reset()
		root2.SetOut(&stdout)
		root2.SetErr(&stderr)
		root2.SetArgs([]string{"install", "claude", "--settings", settingsPath, "--binary", "/x/donegate"})

		if err := root2.Execute(); err != nil {
			t.Fatalf("second install failed: %v", err)
		}
		if !strings.Contains(stdout.String(), "already installed") {
			t.Errorf("stdout = %q, want 'already installed'", stdout.String())
		}

		// Uninstall.
		root3 := newTestInstallRoot()
		stdout.Reset()
		stderr.Reset()
		root3.SetOut(&stdout)
		root3.SetErr(&stderr)
		root3.SetArgs([]string{"uninstall", "claude", "--settings", settingsPath})

		if err := root3.Execute(); err != nil {
			t.Fatalf("uninstall failed: %v", err)
		}
		if !strings.Contains(stdout.String(), "uninstalled Claude Code hooks") {
			t.Errorf("stdout = %q, want uninstalled message", stdout.String())
		}

		afterData, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("read settings after uninstall: %v", err)
		}
		var doc map[string]any
		if err := json.Unmarshal(afterData, &doc); err != nil {
			t.Fatalf("invalid json after uninstall: %v", err)
		}
		if _, exists := doc["hooks"]; exists {
			t.Errorf("expected hooks to be removed after uninstall, got %v", doc["hooks"])
		}
	})

	t.Run("install claude with --no-subagent omits SubagentStop", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		settingsPath := filepath.Join(tmp, "settings.json")

		root := newTestInstallRoot()
		var stdout, stderr bytes.Buffer
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		root.SetArgs([]string{"install", "claude", "--settings", settingsPath, "--binary", "/x/donegate", "--no-subagent"})

		if err := root.Execute(); err != nil {
			t.Fatalf("install --no-subagent failed: %v", err)
		}
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "SubagentStop") {
			t.Errorf("expected SubagentStop to be omitted:\n%s", string(data))
		}
	})

	t.Run("install claude with --dry-run prints JSON and writes nothing", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		settingsPath := filepath.Join(tmp, "dry", "settings.json")

		root := newTestInstallRoot()
		var stdout, stderr bytes.Buffer
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		root.SetArgs([]string{"install", "claude", "--settings", settingsPath, "--binary", "/x/donegate", "--dry-run"})

		if err := root.Execute(); err != nil {
			t.Fatalf("install --dry-run failed: %v", err)
		}
		var doc map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
			t.Fatalf("dry-run stdout is not valid JSON: %v\noutput:\n%s", err, stdout.String())
		}
		if _, exists := doc["hooks"]; !exists {
			t.Errorf("expected hooks in dry-run JSON:\n%s", stdout.String())
		}
		if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
			t.Errorf("dry-run should not write file, stat err = %v", err)
		}
	})

	t.Run("unknown harnesses return not supported yet error (exit 2)", func(t *testing.T) {
		cases := []struct {
			subcmd  string
			harness string
		}{
			{subcmd: "install", harness: "cursor"},
			{subcmd: "uninstall", harness: "cursor"},
		}
		// opencode and pi are supported now; isolate HOME anyway so a regression can never touch the real one.
		t.Setenv("HOME", t.TempDir())

		for _, tc := range cases {
			t.Run(tc.subcmd+"_"+tc.harness, func(t *testing.T) {
				root := newTestInstallRoot()
				var stdout, stderr bytes.Buffer
				root.SetOut(&stdout)
				root.SetErr(&stderr)
				root.SetArgs([]string{tc.subcmd, tc.harness})

				err := root.Execute()
				if err == nil {
					t.Fatalf("expected error for %s %s, got nil", tc.subcmd, tc.harness)
				}
				if errors.Is(err, errVerifyFailed) {
					t.Errorf("error must not be errVerifyFailed (exit 1); should map to exit 2")
				}
				if !strings.Contains(err.Error(), "not supported yet") {
					t.Errorf("error = %q, want substring 'not supported yet'", err.Error())
				}
			})
		}
	})
}

func TestInstallAndUninstallShimCmd(t *testing.T) {
	harnesses := []string{"opencode", "pi"}

	for _, harness := range harnesses {
		t.Run(harness+"/install idempotent reinstall and uninstall", func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("HOME", tmp)
			dir := filepath.Join(tmp, harness+"-dir")
			wantFile := filepath.Join(dir, "donegate.ts")

			// Fresh install.
			root := newTestInstallRoot()
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs([]string{"install", harness, "--dir", dir, "--binary", "/x/donegate"})

			if err := root.Execute(); err != nil {
				t.Fatalf("install %s failed: %v (stderr=%s)", harness, err, stderr.String())
			}
			if !strings.Contains(stdout.String(), "installed") || !strings.Contains(stdout.String(), wantFile) {
				t.Errorf("stdout = %q, want installed message with %q", stdout.String(), wantFile)
			}

			data, err := os.ReadFile(wantFile)
			if err != nil {
				t.Fatalf("read installed shim: %v", err)
			}
			if !strings.Contains(string(data), `"/x/donegate"`) {
				t.Errorf("expected baked binary path in shim:\n%s", string(data))
			}

			// Idempotent second install.
			root2 := newTestInstallRoot()
			stdout.Reset()
			stderr.Reset()
			root2.SetOut(&stdout)
			root2.SetErr(&stderr)
			root2.SetArgs([]string{"install", harness, "--dir", dir, "--binary", "/x/donegate"})

			if err := root2.Execute(); err != nil {
				t.Fatalf("second install %s failed: %v", harness, err)
			}
			if !strings.Contains(stdout.String(), "already installed") {
				t.Errorf("stdout = %q, want 'already installed'", stdout.String())
			}

			// Uninstall.
			root3 := newTestInstallRoot()
			stdout.Reset()
			stderr.Reset()
			root3.SetOut(&stdout)
			root3.SetErr(&stderr)
			root3.SetArgs([]string{"uninstall", harness, "--dir", dir})

			if err := root3.Execute(); err != nil {
				t.Fatalf("uninstall %s failed: %v", harness, err)
			}
			if !strings.Contains(stdout.String(), "uninstalled") {
				t.Errorf("stdout = %q, want 'uninstalled'", stdout.String())
			}
			if _, err := os.Stat(wantFile); !os.IsNotExist(err) {
				t.Errorf("expected %s to be removed, stat err = %v", wantFile, err)
			}

			// Second uninstall is a no-op.
			root4 := newTestInstallRoot()
			stdout.Reset()
			stderr.Reset()
			root4.SetOut(&stdout)
			root4.SetErr(&stderr)
			root4.SetArgs([]string{"uninstall", harness, "--dir", dir})

			if err := root4.Execute(); err != nil {
				t.Fatalf("second uninstall %s failed: %v", harness, err)
			}
			if !strings.Contains(stdout.String(), "no ") {
				t.Errorf("stdout = %q, want 'no ... found' message", stdout.String())
			}
		})

		t.Run(harness+"/dry-run writes nothing", func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("HOME", tmp)
			dir := filepath.Join(tmp, "dry-"+harness)

			root := newTestInstallRoot()
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs([]string{"install", harness, "--dir", dir, "--binary", "/x/donegate", "--dry-run"})

			if err := root.Execute(); err != nil {
				t.Fatalf("dry-run install %s failed: %v", harness, err)
			}
			if !strings.Contains(stdout.String(), "would install") {
				t.Errorf("stdout = %q, want 'would install'", stdout.String())
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Errorf("dry-run should not create dir, stat err = %v", err)
			}
		})
	}

	t.Run("truly unknown harness returns not supported yet", func(t *testing.T) {
		for _, subcmd := range []string{"install", "uninstall"} {
			root := newTestInstallRoot()
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs([]string{subcmd, "unknown-harness"})

			err := root.Execute()
			if err == nil {
				t.Fatalf("expected error for %s unknown-harness, got nil", subcmd)
			}
			if errors.Is(err, errVerifyFailed) {
				t.Errorf("error must not be errVerifyFailed")
			}
			if !strings.Contains(err.Error(), "not supported yet") {
				t.Errorf("error = %q, want substring 'not supported yet'", err.Error())
			}
		}
	})
}

func TestInstallAndUninstallAgyCmd(t *testing.T) {
	for _, harness := range []string{"agy", "antigravity"} {
		t.Run(harness+"/install with --dir, idempotent reinstall, and uninstall", func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("HOME", tmp)
			dir := filepath.Join(tmp, ".agents")
			hooksPath := filepath.Join(dir, "hooks.json")

			root := newTestInstallRoot()
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs([]string{"install", harness, "--dir", dir, "--binary", "/x/donegate"})

			if err := root.Execute(); err != nil {
				t.Fatalf("install %s failed: %v (stderr=%s)", harness, err, stderr.String())
			}
			if !strings.Contains(stdout.String(), "installed Antigravity hooks in "+hooksPath) {
				t.Errorf("stdout = %q, want installed message with %q", stdout.String(), hooksPath)
			}

			data, err := os.ReadFile(hooksPath)
			if err != nil {
				t.Fatalf("read hooks.json: %v", err)
			}
			if !strings.Contains(string(data), "/x/donegate guard --format agy") {
				t.Errorf("expected agy guard hook in hooks.json:\n%s", string(data))
			}

			// Idempotent second install.
			root2 := newTestInstallRoot()
			stdout.Reset()
			stderr.Reset()
			root2.SetOut(&stdout)
			root2.SetErr(&stderr)
			root2.SetArgs([]string{"install", harness, "--dir", dir, "--binary", "/x/donegate"})

			if err := root2.Execute(); err != nil {
				t.Fatalf("second install %s failed: %v", harness, err)
			}
			if !strings.Contains(stdout.String(), "already installed") {
				t.Errorf("stdout = %q, want 'already installed'", stdout.String())
			}

			// Uninstall.
			root3 := newTestInstallRoot()
			stdout.Reset()
			stderr.Reset()
			root3.SetOut(&stdout)
			root3.SetErr(&stderr)
			root3.SetArgs([]string{"uninstall", harness, "--dir", dir})

			if err := root3.Execute(); err != nil {
				t.Fatalf("uninstall %s failed: %v", harness, err)
			}
			if !strings.Contains(stdout.String(), "uninstalled Antigravity hooks") {
				t.Errorf("stdout = %q, want uninstalled message", stdout.String())
			}

			afterData, err := os.ReadFile(hooksPath)
			if err != nil {
				t.Fatalf("read hooks.json after uninstall: %v", err)
			}
			var doc map[string]any
			if err := json.Unmarshal(afterData, &doc); err != nil {
				t.Fatalf("invalid json after uninstall: %v", err)
			}
			if _, exists := doc["donegate"]; exists {
				t.Errorf("expected donegate key to be removed, got %v", doc)
			}

			// Second uninstall is a no-op.
			root4 := newTestInstallRoot()
			stdout.Reset()
			stderr.Reset()
			root4.SetOut(&stdout)
			root4.SetErr(&stderr)
			root4.SetArgs([]string{"uninstall", harness, "--dir", dir})

			if err := root4.Execute(); err != nil {
				t.Fatalf("second uninstall %s failed: %v", harness, err)
			}
			if !strings.Contains(stdout.String(), "no Antigravity hooks found") {
				t.Errorf("stdout = %q, want 'no Antigravity hooks found'", stdout.String())
			}
		})
	}

	t.Run("install agy default HOME path and dry-run", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		wantDefaultPath := filepath.Join(tmp, ".gemini", "config", "hooks.json")

		root := newTestInstallRoot()
		var stdout, stderr bytes.Buffer
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		root.SetArgs([]string{"install", "agy", "--binary", "/x/donegate", "--dry-run"})

		if err := root.Execute(); err != nil {
			t.Fatalf("install agy --dry-run failed: %v", err)
		}
		var doc map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
			t.Fatalf("dry-run stdout is not valid JSON: %v\noutput:\n%s", err, stdout.String())
		}
		if _, exists := doc["donegate"]; !exists {
			t.Errorf("expected donegate key in dry-run JSON:\n%s", stdout.String())
		}
		if _, err := os.Stat(wantDefaultPath); !os.IsNotExist(err) {
			t.Errorf("dry-run should not write file, stat err = %v", err)
		}

		// Real install to default HOME path.
		root2 := newTestInstallRoot()
		stdout.Reset()
		stderr.Reset()
		root2.SetOut(&stdout)
		root2.SetErr(&stderr)
		root2.SetArgs([]string{"install", "agy", "--binary", "/x/donegate"})

		if err := root2.Execute(); err != nil {
			t.Fatalf("install agy default path failed: %v", err)
		}
		if !strings.Contains(stdout.String(), wantDefaultPath) {
			t.Errorf("stdout = %q, want path %q", stdout.String(), wantDefaultPath)
		}
		if _, err := os.Stat(wantDefaultPath); err != nil {
			t.Errorf("expected file at %s: %v", wantDefaultPath, err)
		}
	})
}
