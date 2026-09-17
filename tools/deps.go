//go:build tools

// Package tools pins dependencies that are only used by the helper programs in
// this directory.
//
// tools/build-version.go and tools/build-date.go carry the "ignore" build
// constraint so that each can be a standalone `package main` run via `go run`.
// `go mod tidy` skips ignore-constrained files entirely, so without this file it
// drops github.com/blang/semver/v4 from go.mod and `make build` then fails with
// "no required module provides package". Custom build tags such as "tools" are
// considered by tidy, so a blank import here keeps the requirement.
package tools

import _ "github.com/blang/semver/v4"
