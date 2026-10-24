package embedded

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// CleanStaleMCPEntry removes the "liza" key from the "mcpServers" object in
// .mcp.json at the project root. Prior Liza versions wrote this entry during
// init; the MCP server has since been removed. If the file becomes empty
// (no remaining servers), it is deleted entirely.
func CleanStaleMCPEntry(projectRoot string) error {
	mcpPath := filepath.Join(projectRoot, ".mcp.json")
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading .mcp.json: %w", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil // not valid JSON — leave it alone
	}

	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		return nil
	}
	if _, hasLiza := servers["liza"]; !hasLiza {
		return nil
	}

	delete(servers, "liza")

	if len(servers) == 0 && len(doc) == 1 {
		// Only mcpServers key remained, and it's now empty — remove file
		return os.Remove(mcpPath)
	}

	if len(servers) == 0 {
		delete(doc, "mcpServers")
	}

	output, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling .mcp.json: %w", err)
	}
	return os.WriteFile(mcpPath, append(output, '\n'), 0644)
}
