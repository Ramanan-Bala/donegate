package verify

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/rules"
)

type expectedFixture struct {
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Findings    []struct {
		Rule string `json:"rule"`
		Path string `json:"path"`
	} `json:"findings"`
}

type rulePathKey struct {
	rule string
	path string
}

func TestFixtures(t *testing.T) {
	fixturesDir, ok := findFixturesDir(t)
	if !ok {
		t.Skip("testdata/fixtures does not exist")
	}

	baseDir := filepath.Join(fixturesDir, "_base")
	if info, err := os.Stat(baseDir); err != nil || !info.IsDir() {
		t.Skip("testdata/fixtures/_base does not exist")
	}

	entries, err := os.ReadDir(fixturesDir)
	if err != nil {
		t.Fatalf("read fixtures dir %s: %v", fixturesDir, err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
			continue
		}
		fixtureDir := filepath.Join(fixturesDir, name)
		t.Run(name, func(t *testing.T) {
			expPath := filepath.Join(fixtureDir, "expected.json")
			expBytes, err := os.ReadFile(expPath)
			if err != nil {
				t.Fatalf("read %s: %v", expPath, err)
			}
			var exp expectedFixture
			if err := json.Unmarshal(expBytes, &exp); err != nil {
				t.Fatalf("parse %s: %v", expPath, err)
			}

			missingSet := map[string]struct{}{}
			wantSet := map[rulePathKey]struct{}{}
			for _, f := range exp.Findings {
				if _, registered := rules.Lookup(f.Rule); !registered {
					missingSet[f.Rule] = struct{}{}
				}
				wantSet[rulePathKey{rule: f.Rule, path: filepath.ToSlash(f.Path)}] = struct{}{}
			}
			if len(missingSet) > 0 {
				missing := make([]string, 0, len(missingSet))
				for id := range missingSet {
					missing = append(missing, id)
				}
				sort.Strings(missing)
				t.Skipf("rules not implemented yet: %s", strings.Join(missing, ", "))
			}

			repo := t.TempDir()
			if err := copyDir(baseDir, repo); err != nil {
				t.Fatalf("copy _base: %v", err)
			}

			runGit(t, repo, "-c", "init.defaultBranch=main", "init")
			baseSHA := commitAll(t, repo, "base")

			overlayDir := filepath.Join(fixtureDir, "overlay")
			if info, err := os.Stat(overlayDir); err == nil && info.IsDir() {
				if err := copyDir(overlayDir, repo); err != nil {
					t.Fatalf("apply overlay: %v", err)
				}
			}

			deleteFile := filepath.Join(fixtureDir, "delete.txt")
			if data, err := os.ReadFile(deleteFile); err == nil {
				for _, line := range strings.Split(string(data), "\n") {
					rel := strings.TrimSpace(line)
					if rel == "" {
						continue
					}
					target := filepath.Join(repo, filepath.FromSlash(rel))
					if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
						t.Fatalf("delete %s: %v", rel, err)
					}
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("read delete.txt: %v", err)
			}

			_ = commitAll(t, repo, "fixture")

			res, err := Run(Options{RepoDir: repo, Base: baseSHA})
			if err != nil {
				t.Fatalf("verify.Run failed: %v", err)
			}

			gotSet := map[rulePathKey]struct{}{}
			for _, f := range res.Findings {
				if _, registered := rules.Lookup(f.RuleID); !registered {
					continue
				}
				gotSet[rulePathKey{rule: f.RuleID, path: filepath.ToSlash(f.Path)}] = struct{}{}
			}

			var missingFindings, extraFindings []rulePathKey
			for k := range wantSet {
				if _, ok := gotSet[k]; !ok {
					missingFindings = append(missingFindings, k)
				}
			}
			for k := range gotSet {
				if _, ok := wantSet[k]; !ok {
					extraFindings = append(extraFindings, k)
				}
			}
			if len(missingFindings) > 0 || len(extraFindings) > 0 {
				t.Fatalf("fixture %s (%s) mismatch:\n  missing: %+v\n  extra:   %+v\n  all findings: %+v",
					name, exp.Description, missingFindings, extraFindings, res.Findings)
			}
		})
	}
}

func findFixturesDir(t *testing.T) (string, bool) {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			fixtures := filepath.Join(dir, "testdata", "fixtures")
			info, err := os.Stat(fixtures)
			if err != nil || !info.IsDir() {
				return "", false
			}
			return fixtures, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate go.mod")
		}
		dir = parent
	}
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()

		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer out.Close()

		_, err = io.Copy(out, in)
		return err
	})
}
