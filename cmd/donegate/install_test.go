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
			{subcmd: "install", harness: "opencode"},
			{subcmd: "install", harness: "pi"},
			{subcmd: "uninstall", harness: "opencode"},
			{subcmd: "uninstall", harness: "pi"},
		}

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
