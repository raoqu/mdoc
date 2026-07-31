package themeassets

import "embed"

// Files contains the preview theme styles that ship with the standalone binary.
//
//go:embed *.css
var Files embed.FS
