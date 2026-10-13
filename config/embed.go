// Package config embeds the Pkl schema package so the binary can evaluate
// .pkl config files at runtime with no project structure on disk. Config
// files reference it as @schema/... through bmurray/pkl-loader.
//
// The deploy schema (deploy/) is deliberately not embedded: it is rendered by
// the pkl CLI at build time, never by the application.
package config

import "embed"

//go:embed *.pkl credential/*.pkl auth/*.pkl auth/provider/*.pkl ai/*.pkl ai/provider/*.pkl PklProject PklProject.deps.json
var SchemaFS embed.FS
