// Package payloadschema is the registry of versioned payload schemas for
// commands and lifecycle operations.
//
// A schema's payload is its operation's canonical JSON object: for file-driven
// commands the file content, for flag-driven commands an object whose keys
// mirror the flag names, built by the command before it calls its ops function.
// The preflight command and the mutation boundary validate that same object
// through this package, so a payload cannot pass one and fail the other for
// structural reasons.
//
// Validation is structural only: a schema reads its payload and nothing else —
// no state read, no lock, no Git. The package therefore depends on the models
// vocabulary and the pure state validators alone, a boundary its tests enforce.
//
// The registry holds no schema of its own. Each command owner registers its
// schemas from a sibling file in this package at init time; Register reports a
// malformed or duplicate registration as a programming error by panicking,
// while payload validation never panics and returns bounded diagnostics.
package payloadschema
