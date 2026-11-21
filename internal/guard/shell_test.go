package guard

import (
	"reflect"
	"testing"
)

func TestShellWrites(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		want     []string
		wantSnap bool
	}{
		{
			name:    "output redirect >",
			command: "echo foo > bar.test.ts",
			want:    []string{"bar.test.ts"},
		},
		{
			name:    "append redirect >>",
			command: "echo foo >> bar.test.ts",
			want:    []string{"bar.test.ts"},
		},
		{
			name:    "fd1 redirect 1>",
			command: "echo foo 1> out.txt",
			want:    []string{"out.txt"},
		},
		{
			name:    "fd2 redirect 2>",
			command: "cmd 2> err.log",
			want:    []string{"err.log"},
		},
		{
			name:    "combined redirect &>",
			command: "cmd &> all.log",
			want:    []string{"all.log"},
		},
		{
			name:    "attached redirect >file",
			command: "echo hi>attached.test.ts",
			want:    []string{"attached.test.ts"},
		},
		{
			name:    "redirect to /dev/null ignored",
			command: "cat x > /dev/null 2>&1",
			want:    nil,
		},
		{
			name:    "tee -a multiple files",
			command: "echo x | tee -a a.test.ts b.test.ts",
			want:    []string{"a.test.ts", "b.test.ts"},
		},
		{
			name:    "sed -i gnu style",
			command: "sed -i 's/a/b/' src/cart.test.ts",
			want:    []string{"src/cart.test.ts"},
		},
		{
			name:    "sed -i empty suffix bsd style",
			command: "sed -i '' 's/a/b/' src/cart.test.ts",
			want:    []string{"src/cart.test.ts"},
		},
		{
			name:    "sed -i.bak",
			command: "sed -i.bak 's/a/b/' a.test.ts b.test.ts",
			want:    []string{"a.test.ts", "b.test.ts"},
		},
		{
			name:    "sed --in-place with -e",
			command: "sed --in-place -e 's/a/b/' vitest.config.ts",
			want:    []string{"vitest.config.ts"},
		},
		{
			name:    "sed without -i ignored",
			command: "sed 's/a/b/' src/cart.test.ts",
			want:    nil,
		},
		{
			name:    "perl -pi -e",
			command: "perl -pi -e 's/900/1000/' src/cart.test.ts",
			want:    []string{"src/cart.test.ts"},
		},
		{
			name:    "perl -i.bak -pe",
			command: "perl -i.bak -pe 's/a/b/' a.test.ts",
			want:    []string{"a.test.ts"},
		},
		{
			name:    "mv removes source and writes dest",
			command: "mv -f old.test.ts new.test.ts",
			want:    []string{"old.test.ts", "new.test.ts"},
		},
		{
			name:    "cp collects dest only",
			command: "cp -r src.ts dst.test.ts",
			want:    []string{"dst.test.ts"},
		},
		{
			name:    "rm collects all targets",
			command: "rm -rf a.test.ts b.spec.ts",
			want:    []string{"a.test.ts", "b.spec.ts"},
		},
		{
			name:    "truncate skips -s value",
			command: "truncate -s 0 a.test.ts",
			want:    []string{"a.test.ts"},
		},
		{
			name:    "dd of=<path>",
			command: "dd if=/dev/zero of=out.snap bs=1 count=0",
			want:    []string{"out.snap"},
		},
		{
			name:    "git checkout -- path",
			command: "git checkout -- src/__tests__/cart.test.ts",
			want:    []string{"src/__tests__/cart.test.ts"},
		},
		{
			name:    "git checkout ref -- paths",
			command: "git checkout HEAD -- a.test.ts b.test.ts",
			want:    []string{"a.test.ts", "b.test.ts"},
		},
		{
			name:    "git checkout branch without -- ignored",
			command: "git checkout main",
			want:    nil,
		},
		{
			name:    "git restore paths",
			command: "git restore --source HEAD src/cart.test.ts",
			want:    []string{"src/cart.test.ts"},
		},
		{
			name:     "npx vitest run -u",
			command:  "npx vitest run -u",
			wantSnap: true,
		},
		{
			name:     "npm test -- -u",
			command:  "npm test -- -u",
			wantSnap: true,
		},
		{
			name:     "npm run test -- --update",
			command:  "npm run test -- --update",
			wantSnap: true,
		},
		{
			name:     "yarn test -- -u",
			command:  "yarn test -- -u",
			wantSnap: true,
		},
		{
			name:     "pnpm test -- --updateSnapshot",
			command:  "pnpm test -- --updateSnapshot",
			wantSnap: true,
		},
		{
			name:     "jest --updateSnapshot",
			command:  "jest --updateSnapshot",
			wantSnap: true,
		},
		{
			name:     "bunx vitest -u",
			command:  "bunx vitest -u",
			wantSnap: true,
		},
		{
			name:     "and chain combines writes and snapshot update",
			command:  "cp a.ts b.ts && npx vitest -u",
			want:     []string{"b.ts"},
			wantSnap: true,
		},
		{
			name:    "quoted redirect ignored",
			command: `echo "a > b"`,
			want:    nil,
		},
		{
			name:    "single-quoted separator and command ignored",
			command: `echo 'foo && vitest -u > bar.ts'`,
			want:    nil,
		},
		{
			name:    "escaped redirect ignored",
			command: `echo a \> b.ts`,
			want:    nil,
		},
		{
			name:     "grep -u not a snapshot flag",
			command:  "grep -u pattern src/file.ts",
			want:     nil,
			wantSnap: false,
		},
		{
			name:     "ls -la harmless",
			command:  "ls -la",
			want:     nil,
			wantSnap: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotPaths, gotSnap := ShellWrites(tc.command)
			if !reflect.DeepEqual(gotPaths, tc.want) {
				t.Errorf("ShellWrites(%q) paths = %v, want %v", tc.command, gotPaths, tc.want)
			}
			if gotSnap != tc.wantSnap {
				t.Errorf("ShellWrites(%q) snapshotUpdate = %v, want %v", tc.command, gotSnap, tc.wantSnap)
			}
		})
	}
}
