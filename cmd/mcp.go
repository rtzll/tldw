package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/adrg/xdg"
	"github.com/spf13/cobra"

	"github.com/rtzll/tldw/internal"
)

// mcpCmd represents the mcp command
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run minimal MCP server for TL;DW",
	Long: `Run a Model Context Protocol (MCP) server that exposes TL;DW functionality as tools.

The MCP server provides two tools:
- get_youtube_metadata: Extract video metadata as formatted text
- transcribe_youtube_video: Fetch built-in captions or use Whisper fallback

This allows AI assistants to use TL;DW capabilities through the MCP protocol.

Transport options:
- stdio (default): Standard MCP transport via stdin/stdout
- http: HTTP transport on specified port (use --port to configure)`,
	Example: `  # Run MCP server with stdio transport (e.g. for Claude Desktop)
  tldw mcp

  # Run MCP server with HTTP transport on port 8080
  tldw mcp --transport=http --port=8080

  # Set up Claude Desktop integration
  tldw mcp setup-claude`,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// MCP uses stdio protocol, so disable verbose logging
		config.Verbose = false
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		transport, _ := cmd.Flags().GetString("transport")
		port, _ := cmd.Flags().GetInt("port")

		app := internal.NewApp(config)

		mcpServer := internal.NewMCPServer(app)

		if config.Verbose {
			if transport == "http" {
				fmt.Printf("Starting TL;DW MCP server on HTTP port %d...\n", port)
			} else {
				fmt.Println("Starting TL;DW MCP server on stdio...")
			}
		}

		// Start the server (this will block until context is cancelled)
		return mcpServer.Start(cmd.Context(), transport, port)
	},
}

// setupClaudeCmd represents the setup-claude subcommand
var setupClaudeCmd = &cobra.Command{
	Use:   "setup-claude",
	Short: "Configure Claude Desktop to use TL;DW MCP server",
	Long: `Automatically configure Claude Desktop to use TL;DW as an MCP server.

This command will:
- Detect Claude Desktop installation and config location
- Add TL;DW MCP server configuration to claude_desktop_config.json
- Preserve existing MCP server configurations
- Set appropriate XDG environment variables for the current platform`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return setupClaudeDesktop()
	},
}

// ClaudeDesktopConfig represents the claude_desktop_config.json structure
type ClaudeDesktopConfig struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
}

// MCPServerConfig represents an individual MCP server configuration
type MCPServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// setupClaudeDesktop implements the setup-claude subcommand
func setupClaudeDesktop() error {
	// Get the path to the current binary
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("getting executable path: %w", err)
	}

	// Resolve symlinks to get the actual binary path
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("resolving executable path: %w", err)
	}

	// Get Claude Desktop config path
	configPath, err := getClaudeDesktopConfigPath()
	if err != nil {
		return fmt.Errorf("getting Claude Desktop config path: %w", err)
	}

	// Check if config file exists - abort if it doesn't
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return fmt.Errorf("config for Claude Desktop not found at %s", configPath)
	}

	// Read existing config
	var config ClaudeDesktopConfig
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading existing config: %w", err)
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("parsing existing config: %w", err)
	}

	// Initialize mcpServers map if it doesn't exist
	if config.MCPServers == nil {
		config.MCPServers = make(map[string]MCPServerConfig)
	}

	// Get XDG base paths so internal config can add tldw
	xdgPaths := map[string]string{
		"XDG_DATA_HOME":   xdg.DataHome,
		"XDG_CONFIG_HOME": xdg.ConfigHome,
		"XDG_CACHE_HOME":  xdg.CacheHome,
	}

	// Add/update TL;DW MCP server configuration
	config.MCPServers["tldw"] = MCPServerConfig{
		Command: execPath,
		Args:    []string{"mcp"},
		Env:     xdgPaths,
	}

	// Write updated config back to file
	data, err = json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	fmt.Printf("Successfully configured Claude Desktop MCP server\n")
	fmt.Printf("Restart Claude Desktop to use the TL;DW MCP server\n")

	return nil
}

// getClaudeDesktopConfigPath returns the platform-specific config path for Claude Desktop
func getClaudeDesktopConfigPath() (string, error) {
	var configPath string

	switch runtime.GOOS {
	case "darwin":
		// macOS: ~/Library/Application Support/Claude/claude_desktop_config.json
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configPath = filepath.Join(homeDir, "Library", "Application Support", "Claude", "claude_desktop_config.json")

	case "windows":
		// Windows: %APPDATA%/Claude/claude_desktop_config.json
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", fmt.Errorf("APPDATA environment variable not set")
		}
		configPath = filepath.Join(appData, "Claude", "claude_desktop_config.json")

	case "linux":
		// Linux: ~/.config/Claude/claude_desktop_config.json
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configPath = filepath.Join(homeDir, ".config", "Claude", "claude_desktop_config.json")

	default:
		return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return configPath, nil
}

func init() {
	mcpCmd.Flags().String("transport", "stdio", "Transport protocol (stdio or http)")
	mcpCmd.Flags().Int("port", 8080, "Port for HTTP transport (only used with --transport=http)")
	mcpCmd.AddCommand(setupClaudeCmd)
	rootCmd.AddCommand(mcpCmd)
}
