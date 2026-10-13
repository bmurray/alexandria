//go:build dev

// Package web embeds the built frontend. In dev builds nothing is embedded;
// the server proxies to Vite instead.
package web

import "io/fs"

// FS returns nil: no embedded assets in a dev build.
func FS() fs.FS { return nil }
