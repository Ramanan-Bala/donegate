package l2

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/policy"
	"github.com/Ramanan-Bala/donegate/internal/runner"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", full, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

func copyFixture(t *testing.T, name, dstDir, dstRel string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata/%s: %v", name, err)
	}
	writeFile(t, dstDir, dstRel, string(data))
}

func initRepo(t *testing.T) (repo string, baseSHA string, pol *policy.Policy) {
	t.Helper()
	repo = t.TempDir()
	runGit(t, repo, "-c", "init.defaultBranch=main", "init")
	copyFixture(t, "pass.json", repo, "pass.json")
	copyFixture(t, "fail.json", repo, "fail.json")
	writeFile(t, repo, "app/src/app.js", "export const discount = () => 0;\n")
	writeFile(t, repo, "app/src/app.test.js", "grep -q FIXED src/app.js\n")

	policyYAML := `version: 1
projects:
  - name: web
    root: app
    test: sh -c 'if sh src/app.test.js; then cp ../pass.json {json}; else cp ../fail.json {json}; exit 1; fi'
    typecheck: sh -c 'grep -q FIXED src/app.js'
    lint: sh -c 'grep -q CLEAN src/app.js'
`
	writeFile(t, repo, ".donegate.yml", policyYAML)
	var err error
	pol, err = policy.Parse([]byte(policyYAML))
	if err != nil {
		t.Fatalf("parse policy: %v", err)
	}

	runGit(t, repo, "add", "-A")
	runGit(t, repo, "-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "base")
	baseSHA = strings.TrimSpace(runGit(t, repo, "rev-parse", "HEAD"))
	return repo, baseSHA, pol
}

func TestRunNoProjectsIsNoop(t *testing.T) {
	pol := policy.Default()
	runs, base, err := Run(context.Background(), Options{
		RepoDir: t.TempDir(),
		Base:    "HEAD",
		Policy:  pol,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if runs != nil || base != nil {
		t.Fatalf("expected nil runs and baseline, got runs=%+v base=%+v", runs, base)
	}
}

func TestRunCleanFixPassesAndRestoresProtectedTestRigging(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command tests use POSIX /bin/sh")
	}

	repo, baseSHA, pol := initRepo(t)

	// 1. Agent fixes source cleanly -> test, typecheck, and lint all pass in verification worktree,
	// while baseline at Base still fails.
	writeFile(t, repo, "app/src/app.js", "export const discount = () => 'FIXED CLEAN';\n")

	runs, base, err := Run(context.Background(), Options{
		RepoDir: repo,
		Base:    baseSHA,
		Policy:  pol,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("got %d runs, want 3 (test, typecheck, lint): %+v", len(runs), runs)
	}
	for i, r := range runs {
		if !r.OK() {
			t.Errorf("runs[%d] (%s) expected OK, got %+v", i, r.Kind, r)
		}
		if r.Project != "web" {
			t.Errorf("runs[%d].Project = %q, want web", i, r.Project)
		}
	}
	if runs[0].Format != runner.FormatVitestJSON || runs[0].Total != 2 || runs[0].Passed != 2 {
		t.Errorf("unexpected test run stats: %+v", runs[0])
	}
	if len(base) != 1 || base[0].Kind != runner.Test || base[0].OK() {
		t.Fatalf("expected failing baseline test run, got %+v", base)
	}
	if base[0].Total != 2 || base[0].Failed != 1 {
		t.Errorf("unexpected baseline counts: %+v", base[0])
	}

	// 2. Agent leaves source broken and edits protected test script (simulating rigging).
	// Verification worktree must restore app/src/app.test.js from Base so tests still fail.
	writeFile(t, repo, "app/src/app.js", "export const discount = () => 'BROKEN';\n")
	writeFile(t, repo, "app/src/app.test.js", "exit 0\n")

	runs, _, err = Run(context.Background(), Options{
		RepoDir: repo,
		Base:    baseSHA,
		Policy:  pol,
	})
	if err != nil {
		t.Fatalf("Run rigged test failed: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("got %d runs, want 3", len(runs))
	}
	if runs[0].OK() {
		t.Fatalf("expected restored test to fail in clean worktree, got OK: %+v", runs[0])
	}
	if len(runs[0].Failures) != 1 || runs[0].Failures[0].Name != "app applies 10% discount" {
		t.Errorf("unexpected failures: %+v", runs[0].Failures)
	}
	if runs[1].OK() || runs[2].OK() {
		t.Errorf("expected typecheck and lint to fail: typecheck=%+v lint=%+v", runs[1], runs[2])
	}
}

func TestRunCachesBaselineAndLinksNodeModulesAndLeavesRepoUntouched(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command tests use POSIX /bin/sh")
	}

	repo := t.TempDir()
	runGit(t, repo, "-c", "init.defaultBranch=main", "init")
	copyFixture(t, "pass.json", repo, "pass.json")
	writeFile(t, repo, "app/src/app.js", "FIXED\n")

	counterFile := filepath.Join(t.TempDir(), "counter.txt")
	// Untracked node_modules directory in repo + project root (not committed to git).
	writeFile(t, repo, "app/node_modules/pkg/marker.txt", "ok\n")
	writeFile(t, repo, ".gitignore", "node_modules/\n**/node_modules/\n")

	testCmd := fmt.Sprintf(
		"sh -c 'echo run >> %q && test -f node_modules/pkg/marker.txt && cp ../pass.json {json}'",
		counterFile,
	)
	pol := policy.Default()
	pol.Projects = []policy.Project{
		{
			Root: "app", // Name intentionally empty -> Project falls back to root
			Test: testCmd,
		},
	}

	runGit(t, repo, "add", "-A")
	runGit(t, repo, "-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "base")
	baseSHA := strings.TrimSpace(runGit(t, repo, "rev-parse", "HEAD"))

	headBefore := strings.TrimSpace(runGit(t, repo, "rev-parse", "HEAD"))
	statusBefore := runGit(t, repo, "status", "--porcelain")
	wtBefore := runGit(t, repo, "worktree", "list", "--porcelain")

	cacheDir := t.TempDir()

	// First Run: executes verification worktree (1) + baseline worktree (1) = 2 runs.
	runs1, base1, err := Run(context.Background(), Options{
		RepoDir:  repo,
		Base:     baseSHA,
		Policy:   pol,
		CacheDir: cacheDir,
	})
	if err != nil {
		t.Fatalf("first Run failed: %v", err)
	}
	if len(runs1) != 1 || !runs1[0].OK() {
		t.Fatalf("first runs = %+v, want 1 OK run", runs1)
	}
	if runs1[0].Project != "app" {
		t.Errorf("Project = %q, want app (fallback to root)", runs1[0].Project)
	}
	if len(base1) != 1 || !base1[0].OK() {
		t.Fatalf("first baseline = %+v, want 1 OK run", base1)
	}

	cacheFile := filepath.Join(cacheDir, fmt.Sprintf("baseline-%s.json", baseSHA))
	if _, err := os.Stat(cacheFile); err != nil {
		t.Fatalf("expected baseline cache file %s: %v", cacheFile, err)
	}

	countAfter1 := countLines(t, counterFile)
	if countAfter1 != 2 {
		t.Fatalf("counter after first Run = %d, want 2", countAfter1)
	}

	// Second Run with same CacheDir & Base: reuses cached baseline, runs only verification worktree (1) = 3 total.
	runs2, base2, err := Run(context.Background(), Options{
		RepoDir:  repo,
		Base:     baseSHA,
		Policy:   pol,
		CacheDir: cacheDir,
	})
	if err != nil {
		t.Fatalf("second Run failed: %v", err)
	}
	if len(runs2) != 1 || !runs2[0].OK() || len(base2) != 1 || !base2[0].OK() {
		t.Fatalf("second Run unexpected results: runs=%+v base=%+v", runs2, base2)
	}

	countAfter2 := countLines(t, counterFile)
	if countAfter2 != 3 {
		t.Fatalf("counter after second Run = %d, want 3 (baseline should be cached)", countAfter2)
	}

	headAfter := strings.TrimSpace(runGit(t, repo, "rev-parse", "HEAD"))
	statusAfter := runGit(t, repo, "status", "--porcelain")
	wtAfter := runGit(t, repo, "worktree", "list", "--porcelain")
	cachedIdx := runGit(t, repo, "diff", "--cached")

	if headAfter != headBefore {
		t.Errorf("HEAD changed: %q -> %q", headBefore, headAfter)
	}
	if statusAfter != statusBefore {
		t.Errorf("git status changed:\nbefore:\n%s\nafter:\n%s", statusBefore, statusAfter)
	}
	if wtAfter != wtBefore {
		t.Errorf("git worktree list changed:\nbefore:\n%s\nafter:\n%s", wtBefore, wtAfter)
	}
	if cachedIdx != "" {
		t.Errorf("real repo index is dirty: %q", cachedIdx)
	}
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			n++
		}
	}
	return n
}
