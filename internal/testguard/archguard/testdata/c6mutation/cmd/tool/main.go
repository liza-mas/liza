// Command tool imports the state package under another name.
package main

import store "c6fixture/internal/db"

func main() {
	handle := store.For("x")
	_ = handle.Write(nil)
}
