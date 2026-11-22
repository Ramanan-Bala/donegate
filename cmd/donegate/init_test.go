package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/policy"
)

func TestInitCmd(t *testing.T) {
	t.Run("writes .donegate.yml at git repo root and prints summary", func(t *testing.T) {
		repo := t.TempDir()
		runGitCmd(t, repo, "-c", "init.defaultBranch=main", "init")
		writeTestFile(t, repo, "package.json", `{
  "name": "web-app",
  "scripts": {"lint": "eslint ."},
  "devDependencies": {"vitest": "^2.0.0"}
}`)
		writeTestFile(t, repo, "tsconfig.json", "{}\n")
		writeTestFile(t, repo, "src/index.ts", "export const ok = true;\n")

		// Run from a nested directory to verify repo root resolution via git.
		t.Chdir(filepath.Join(repo, "src"))

		stdout, stderr, err := execCmd(t, "", "init")
		if err != nil {
			t.Fatalf("donegate init failed: %v (stderr=%q)", err, stderr)
		}

		cfgPath := filepath.Join(repo, ".donegate.yml")
		data, readErr := os.ReadFile(cfgPath)
		if readErr != nil {
			t.Fatalf("read %s: %v", cfgPath, readErr)
		}
		if !strings.Contains(stdout, ".donegate.yml") {
			t.Errorf("expected written path in stdout, got %q", stdout)
		}
		if !strings.Contains(stdout, "web-app") {
			t.Errorf("expected project summary in stdout, got %q", stdout)
		}

		pol, parseErr := policy.Parse(data)
		if parseErr != nil {
			t.Fatalf("policy.Parse(.donegate.yml): %v", parseErr)
		}
		if len(pol.Projects) != 1 || pol.Projects[0].Name != "web-app" {
			t.Errorf("unexpected projects in written config: %+v", pol.Projects)
		}
	})

	t.Run("refuses to overwrite without --force and overwrites with --force", func(t *testing.T) {
		repo := t.TempDir()
		runGitCmd(t, repo, "-c", "init.defaultBranch=main", "init")
		writeTestFile(t, repo, "go.mod", "module example.com/demo\n\ngo 1.24\n")
		existing := "# existing config\nversion: 1\n"
		writeTestFile(t, repo, ".donegate.yml", existing)

		t.Chdir(repo)

		_, _, err := execCmd(t, "", "init")
		if err == nil {
			t.Fatalf("expected error when .donegate.yml already exists")
		}
		if !strings.Contains(err.Error(), ".donegate.yml") {
			t.Errorf("error = %q, want it to name .donegate.yml", err.Error())
		}

		gotAfterRefusal, readErr := os.ReadFile(filepath.Join(repo, ".donegate.yml"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(gotAfterRefusal) != existing {
			t.Fatalf("existing .donegate.yml was modified on refusal")
		}

		stdout, stderr, forceErr := execCmd(t, "", "init", "--force")
		if forceErr != nil {
			t.Fatalf("init --force failed: %v (stderr=%q)", forceErr, stderr)
		}
		if !strings.Contains(stdout, ".donegate.yml") || !strings.Contains(stdout, "demo") {
			t.Errorf("unexpected stdout from --force: %q", stdout)
		}

		gotAfterForce, readErr := os.ReadFile(filepath.Join(repo, ".donegate.yml"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(gotAfterForce) == existing {
			t.Fatalf("expected .donegate.yml to be overwritten with --force")
		}
		if _, parseErr := policy.Parse(gotAfterForce); parseErr != nil {
			t.Fatalf("overwritten .donegate.yml invalid: %v", parseErr)
		}
	})

	t.Run("--dry-run prints YAML to stdout and writes nothing", func(t *testing.T) {
		repo := t.TempDir()
		runGitCmd(t, repo, "-c", "init.defaultBranch=main", "init")
		writeTestFile(t, repo, "package.json", `{
  "name": "api",
  "devDependencies": {"jest": "^29.0.0"}
}`)

		t.Chdir(repo)

		stdout, stderr, err := execCmd(t, "", "init", "--dry-run")
		if err != nil {
			t.Fatalf("init --dry-run failed: %v (stderr=%q)", err, stderr)
		}

		if _, statErr := os.Stat(filepath.Join(repo, ".donegate.yml")); !os.IsNotExist(statErr) {
			t.Fatalf("expected .donegate.yml not to be created on --dry-run, statErr=%v", statErr)
		}

		pol, parseErr := policy.Parse([]byte(stdout))
		if parseErr != nil {
			t.Fatalf("dry-run stdout is not valid policy YAML: %v\nstdout:\n%s", parseErr, stdout)
		}
		if len(pol.Projects) != 1 || pol.Projects[0].Name != "api" {
			t.Errorf("unexpected projects in dry-run output: %+v", pol.Projects)
		}
	})
}
