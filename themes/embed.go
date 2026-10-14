package themeassets

import "embed"

// Files contains native Hugo themes and shared components for previews and publication.
//
//go:embed all:*
var Files embed.FS
