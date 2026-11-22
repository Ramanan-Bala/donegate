package initcfg

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/policy"
)

func TestDetect(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T, root string)
		wantPM    string
		wantProjs []policy.Project
		wantErr   string
	}{
		{
			name: "npm and jest single project",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, root, "package.json", `{
  "name": "shop-api",
  "scripts": {
    "test": "jest",
    "lint": "eslint ."
  },
  "devDependencies": {
    "jest": "^29.0.0"
  }
}`)
				writeFile(t, root, "tsconfig.json", "{}\n")
			},
			wantPM: "npm",
			wantProjs: []policy.Project{
				{
					Name:      "shop-api",
					Root:      "",
					Test:      "npx jest --json --outputFile={json}",
					Typecheck: "npx tsc --noEmit",
					Lint:      "npm run lint",
				},
			},
		},
		{
			name: "pnpm workspace with vitest and tsc",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
				writeFile(t, root, "pnpm-workspace.yaml", "packages:\n  - 'apps/*'\n  - 'packages/*'\n  - '!packages/ignored'\n")
				writeFile(t, root, "package.json", `{"name": "monorepo", "private": true}`)

				writeFile(t, root, "apps/web/package.json", `{
  "name": "@repo/web",
  "scripts": {
    "lint": "eslint src"
  },
  "devDependencies": {
    "vitest": "^2.0.0"
  }
}`)
				writeFile(t, root, "apps/web/tsconfig.json", "{}\n")

				writeFile(t, root, "packages/shared/package.json", `{
  "scripts": {
    "test": "node --test"
  }
}`)
				writeFile(t, root, "packages/ignored/package.json", `{"name": "ignored"}`)
			},
			wantPM: "pnpm",
			wantProjs: []policy.Project{
				{
					Name:      "@repo/web",
					Root:      "apps/web",
					Test:      "pnpm exec vitest run --reporter=json --outputFile={json}",
					Typecheck: "pnpm exec tsc --noEmit",
					Lint:      "pnpm run lint",
				},
				{
					Name: "shared",
					Root: "packages/shared",
					Test: "pnpm test",
				},
			},
		},
		{
			name: "yarn workspaces object form",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, root, "yarn.lock", "# yarn lockfile v1\n")
				writeFile(t, root, "package.json", `{
  "name": "yarn-mono",
  "private": true,
  "workspaces": {
    "packages": ["packages/**"]
  }
}`)
				writeFile(t, root, "packages/api/package.json", `{
  "name": "api",
  "scripts": {
    "lint": "eslint ."
  }
}`)
				writeFile(t, root, "packages/api/jest.config.js", "module.exports = {};\n")
				writeFile(t, root, "packages/ui/package.json", `{
  "name": "ui"
}`)
				writeFile(t, root, "packages/ui/vitest.config.ts", "export default {};\n")
				writeFile(t, root, "packages/ui/tsconfig.json", "{}\n")
				// node_modules inside workspace tree must be ignored
				writeFile(t, root, "packages/ui/node_modules/dep/package.json", `{"name": "dep"}`)
			},
			wantPM: "yarn",
			wantProjs: []policy.Project{
				{
					Name: "api",
					Root: "packages/api",
					Test: "yarn jest --json --outputFile={json}",
					Lint: "yarn run lint",
				},
				{
					Name:      "ui",
					Root:      "packages/ui",
					Test:      "yarn vitest run --reporter=json --outputFile={json}",
					Typecheck: "yarn tsc --noEmit",
				},
			},
		},
		{
			name: "bun single project without test runner or scripts",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, root, "bun.lock", "")
				writeFile(t, root, "package.json", `{"name": "bun-svc"}`)
				writeFile(t, root, "tsconfig.json", "{}\n")
			},
			wantPM: "bun",
			wantProjs: []policy.Project{
				{
					Name:      "bun-svc",
					Root:      "",
					Typecheck: "bunx tsc --noEmit",
				},
			},
		},
		{
			name: "go.mod fallback",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, root, "go.mod", "module github.com/acme/service\n\ngo 1.24\n")
			},
			wantPM: "",
			wantProjs: []policy.Project{
				{
					Name:      "service",
					Root:      "",
					Test:      "go test -count=1 ./...",
					Typecheck: "go vet ./...",
				},
			},
		},
		{
			name:    "empty dir error",
			setup:   func(t *testing.T, root string) {},
			wantErr: "no supported project found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			tc.setup(t, root)

			res, err := Detect(root)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Detect(%q) failed: %v", root, err)
			}
			if res.PackageManager != tc.wantPM {
				t.Errorf("PackageManager = %q, want %q", res.PackageManager, tc.wantPM)
			}
			if !reflect.DeepEqual(res.Projects, tc.wantProjs) {
				t.Errorf("Projects =\n  %+v\nwant\n  %+v", res.Projects, tc.wantProjs)
			}
		})
	}
}

func TestRenderRoundTrip(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	writeFile(t, root, "package.json", `{
  "name": "mono",
  "workspaces": ["packages/*"]
}`)
	writeFile(t, root, "packages/web/package.json", `{
  "name": "web",
  "devDependencies": {"vitest": "^2.0.0"}
}`)
	writeFile(t, root, "packages/web/tsconfig.json", "{}\n")

	res, err := Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	rendered, err := Render(res)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	yamlText := string(rendered)
	if !strings.HasPrefix(yamlText, "# ") {
		t.Errorf("expected header comment at start of YAML, got:\n%s", yamlText)
	}
	if strings.Contains(yamlText, "lint:") {
		t.Errorf("expected empty lint field to be omitted, got:\n%s", yamlText)
	}

	parsed, err := policy.Parse(rendered)
	if err != nil {
		t.Fatalf("policy.Parse(rendered) failed: %v\nYAML:\n%s", err, yamlText)
	}

	def := policy.Default()
	if parsed.Version != def.Version {
		t.Errorf("Version = %d, want %d", parsed.Version, def.Version)
	}
	if !reflect.DeepEqual(parsed.Projects, res.Projects) {
		t.Errorf("parsed Projects = %+v, want %+v", parsed.Projects, res.Projects)
	}
	if !reflect.DeepEqual(parsed.Protect, def.Protect) {
		t.Errorf("parsed Protect = %+v, want %+v", parsed.Protect, def.Protect)
	}
	if !reflect.DeepEqual(parsed.Gate, def.Gate) {
		t.Errorf("parsed Gate = %+v, want %+v", parsed.Gate, def.Gate)
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", full, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}
