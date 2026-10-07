package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// updateClaudeDesktopConfig changes only tldw's managed fields, preserving
// unrelated settings, server definitions, and custom environment variables.
func updateClaudeDesktopConfig(path string, server mcpServerConfig) error {
	// Replace the target atomically without replacing a user's config symlink.
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("locating existing Claude Desktop config: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("reading config permissions: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading existing config: %w", err)
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("parsing existing config: %w", err)
	}
	if config == nil {
		return fmt.Errorf("claude desktop config must be a JSON object")
	}
	servers, err := claudeConfigObject(config["mcpServers"])
	if err != nil {
		return fmt.Errorf("parsing mcpServers: %w", err)
	}
	entry, err := claudeConfigObject(servers["tldw"])
	if err != nil {
		return fmt.Errorf("parsing tldw server config: %w", err)
	}
	env, err := claudeConfigObject(entry["env"])
	if err != nil {
		return fmt.Errorf("parsing tldw environment: %w", err)
	}
	for key, value := range server.Env {
		env[key], err = json.Marshal(value)
		if err != nil {
			return fmt.Errorf("marshaling environment: %w", err)
		}
	}
	for key, value := range map[string]any{"command": server.Command, "args": server.Args, "env": env} {
		entry[key], err = json.Marshal(value)
		if err != nil {
			return fmt.Errorf("marshaling tldw server config: %w", err)
		}
	}
	servers["tldw"], err = json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling tldw server config: %w", err)
	}
	config["mcpServers"], err = json.Marshal(servers)
	if err != nil {
		return fmt.Errorf("marshaling mcpServers: %w", err)
	}
	data, err = json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	return writeClaudeConfigAtomically(path, data, info.Mode().Perm())
}

func claudeConfigObject(data json.RawMessage) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if len(data) > 0 {
		if err := json.Unmarshal(data, &object); err != nil {
			return nil, err
		}
	}
	if object == nil {
		object = make(map[string]json.RawMessage)
	}
	return object, nil
}

func writeClaudeConfigAtomically(path string, data []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".tldw-claude-*")
	if err != nil {
		return fmt.Errorf("creating temporary config: %w", err)
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	if err := file.Chmod(mode); err != nil {
		return fmt.Errorf("setting config permissions: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("writing temporary config: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("syncing config: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("closing config: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replacing config: %w", err)
	}
	return nil
}
