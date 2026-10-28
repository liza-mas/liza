// This scripted peer uses only the standard library. The parent must observe
// the callback before replying; no sleeps or installed ACP host are involved.
package main

import (
	"encoding/json"
	"os"
)

func main() {
	enc := json.NewEncoder(os.Stdout)
	if enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 7, "method": "fs/read_text_file", "params": map[string]any{"sessionId": "fixture-session", "path": "fixture.txt"}}) != nil {
		os.Exit(2)
	}
	var response struct {
		ID     int `json:"id"`
		Result struct {
			Content string `json:"content"`
		} `json:"result"`
	}
	if json.NewDecoder(os.Stdin).Decode(&response) != nil || response.ID != 7 || response.Result.Content == "" {
		os.Exit(3)
	}
	if enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "fixture/observed", "params": map[string]any{"id": response.ID, "content": response.Result.Content}}) != nil {
		os.Exit(4)
	}
}
