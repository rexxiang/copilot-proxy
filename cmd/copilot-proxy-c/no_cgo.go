//go:build !cgo
// +build !cgo

package main

// main exists so the main package compiles even when CGO is disabled.
func main() {}
