package shims

import _ "embed"

//go:embed opencode/donegate.ts
var OpenCode []byte

//go:embed pi/donegate.ts
var Pi []byte
