// Package testhelpers is test support and exempt from the guard.
package testhelpers

import "c6fixture/internal/db"

// Seed writes a fixture state.
func Seed(path string) error { return db.For(path).Write(nil) }
