package install

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type shimHarness struct {
	name            string
	defaultRelPath  string
	install         func(ShimOptions) (string, bool, error)
	uninstall       func(ShimOptions) (string, bool, error)
	expectedSnippet string
}

var shimHarnesses = []shimHarness{
	{
		name:            "opencode",
		defaultRelPath:  filepath.Join(".config", "opencode", "plugins", "donegate.ts"),
		install:         InstallOpenCode,
		uninstall:       UninstallOpenCode,
		expectedSnippet: "tool.execute.before",
	},
	{
		name:            "pi",
		defaultRelPath:  filepath.Join(".pi", "agent", "extensions", "donegate.ts"),
		install:         InstallPi,
		uninstall:       UninstallPi,
		expectedSnippet: "agent_before_settle",
	},
}

func TestInstallShim_FreshAndIdempotent(t *testing.T) {
	for _, h := range shimHarnesses {
		t.Run(h.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			wantPath := filepath.Join(home, h.defaultRelPath)

			gotPath, changed, err := h.install(ShimOptions{
				Binary: "/abs/path/donegate",
			})
			if err != nil {
				t.Fatalf("install failed: %v", err)
			}
			if gotPath != wantPath {
				t.Errorf("path = %q, want %q", gotPath, wantPath)
			}
			if !changed {
				t.Fatalf("expected changed=true on fresh install")
			}

			info, err := os.Stat(gotPath)
			if err != nil {
				t.Fatalf("stat installed shim: %v", err)
			}
			if got := info.Mode().Perm(); got != 0o644 {
				t.Errorf("file mode = %#o, want 0o644", got)
			}

			data, err := os.ReadFile(gotPath)
			if err != nil {
				t.Fatalf("read shim: %v", err)
			}
			content := string(data)
			if !strings.HasPrefix(content, "// donegate shim\n") {
				t.Errorf("expected marker on line 1, got prefix %q", content[:min(len(content), 40)])
			}
			if strings.Contains(content, "__DONEGATE_BIN__") {
				t.Errorf("placeholder __DONEGATE_BIN__ was not replaced")
			}
			if !strings.Contains(content, `"/abs/path/donegate"`) {
				t.Errorf("expected JSON-quoted binary path in shim")
			}
			if !strings.Contains(content, h.expectedSnippet) {
				t.Errorf("expected %q in shim", h.expectedSnippet)
			}

			// Second install with identical options must be idempotent.
			gotPath2, changed2, err := h.install(ShimOptions{
				Binary: "/abs/path/donegate",
			})
			if err != nil {
				t.Fatalf("second install failed: %v", err)
			}
			if gotPath2 != wantPath {
				t.Errorf("second path = %q, want %q", gotPath2, wantPath)
			}
			if changed2 {
				t.Errorf("expected changed=false on idempotent reinstall")
			}

			// Reinstall with a different binary path updates the file.
			_, changed3, err := h.install(ShimOptions{
				Binary: `/Users/alice/My "Tools"/donegate`,
			})
			if err != nil {
				t.Fatalf("reinstall with new binary failed: %v", err)
			}
			if !changed3 {
				t.Errorf("expected changed=true when binary path changes")
			}
			updated, err := os.ReadFile(gotPath)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(updated), `"/Users/alice/My \"Tools\"/donegate"`) {
				t.Errorf("expected JSON-quoted escaped binary path, got:\n%s", string(updated))
			}
			if strings.Contains(string(updated), `"/abs/path/donegate"`) {
				t.Errorf("old binary path still present after update")
			}
		})
	}
}

func TestInstallShim_CustomDirAndDryRun(t *testing.T) {
	for _, h := range shimHarnesses {
		t.Run(h.name, func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("HOME", tmp)
			targetDir := filepath.Join(tmp, "custom", "plugins")
			wantPath := filepath.Join(targetDir, "donegate.ts")

			gotPath, changed, err := h.install(ShimOptions{
				Dir:    targetDir,
				Binary: "/x/donegate",
				DryRun: true,
			})
			if err != nil {
				t.Fatalf("dry-run install failed: %v", err)
			}
			if gotPath != wantPath {
				t.Errorf("path = %q, want %q", gotPath, wantPath)
			}
			if !changed {
				t.Errorf("expected changed=true on dry-run for missing file")
			}
			if _, err := os.Stat(targetDir); !os.IsNotExist(err) {
				t.Errorf("dry-run must not create target dir, stat err = %v", err)
			}
		})
	}
}

func TestUninstallShim(t *testing.T) {
	for _, h := range shimHarnesses {
		t.Run(h.name+"/removes marked shim and is idempotent", func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("HOME", tmp)
			dir := filepath.Join(tmp, "ext")

			path, _, err := h.install(ShimOptions{Dir: dir, Binary: "/x/donegate"})
			if err != nil {
				t.Fatal(err)
			}

			gotPath, removed, err := h.uninstall(ShimOptions{Dir: dir})
			if err != nil {
				t.Fatalf("uninstall failed: %v", err)
			}
			if gotPath != path {
				t.Errorf("path = %q, want %q", gotPath, path)
			}
			if !removed {
				t.Fatalf("expected removed=true")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("expected shim file to be removed, stat err = %v", err)
			}

			// Second uninstall on missing file is a no-op.
			_, removed2, err := h.uninstall(ShimOptions{Dir: dir})
			if err != nil {
				t.Fatalf("second uninstall failed: %v", err)
			}
			if removed2 {
				t.Errorf("expected removed=false on missing file")
			}
		})

		t.Run(h.name+"/preserves foreign file without marker", func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("HOME", tmp)
			dir := filepath.Join(tmp, "ext")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}

			foreignPath := filepath.Join(dir, "donegate.ts")
			foreignContent := []byte("// user custom plugin\nexport default {};\n")
			if err := os.WriteFile(foreignPath, foreignContent, 0o644); err != nil {
				t.Fatal(err)
			}

			gotPath, removed, err := h.uninstall(ShimOptions{Dir: dir})
			if err != nil {
				t.Fatalf("uninstall foreign file returned error: %v", err)
			}
			if gotPath != foreignPath {
				t.Errorf("path = %q, want %q", gotPath, foreignPath)
			}
			if removed {
				t.Errorf("expected removed=false for foreign file without marker")
			}

			after, err := os.ReadFile(foreignPath)
			if err != nil {
				t.Fatalf("foreign file was deleted or unreadable: %v", err)
			}
			if !bytes.Equal(after, foreignContent) {
				t.Errorf("foreign file was modified")
			}
		})
	}
}
