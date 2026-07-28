// This repository is a multi-module Go SDK. There is no importable package at
// the repository root — install a provider submodule instead, for example:
//
//	go get github.com/revenium/revenium-go-sdk/openai
//
// This file exists so that the repository root declares itself explicitly.
// Without it, the Go toolchain synthesises a module from the bare repository
// root at every tag, which resolves successfully and delivers no code — a
// silent no-op that is worse for a caller than an outright failure. Every root
// version published that way is retracted below.
module github.com/revenium/revenium-go-sdk

go 1.23

retract (
	v1.1.5 // No importable code at the module root; use a provider submodule.
	v1.1.4 // No importable code at the module root; use a provider submodule.
	v1.1.3 // No importable code at the module root; use a provider submodule.
	v1.1.2 // No importable code at the module root; use a provider submodule.
	v1.0.2 // No importable code at the module root; use a provider submodule.
	v1.0.1 // No importable code at the module root; use a provider submodule.
	v1.0.0 // No importable code at the module root; use a provider submodule.
)
