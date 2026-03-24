// tools.go — Pins CLI tool versions used across all environments.
//
// This file uses a build constraint so it is never compiled into the
// production binary. Its only purpose is to make `go mod tidy` track
// these modules in go.mod / go.sum, so that `go run <module>` always
// uses the exact same version everywhere: local dev, CI, staging, prod.
//
// HOW IT WORKS:
//   1. Blank imports force `go mod tidy` to keep these in go.sum.
//   2. The Makefile uses `go run <module>` instead of a global binary.
//   3. Go caches the download — first run is slow, subsequent runs instant.
//
// TO ADD A NEW TOOL:
//   1. Add a blank import below.
//   2. Run: cd api && go mod tidy
//   3. Use: go run <module-path> in the Makefile.

//go:build tools

package tools

import (
	_ "github.com/pressly/goose/v3/cmd/goose"
	_ "github.com/sqlc-dev/sqlc/cmd/sqlc"
)
