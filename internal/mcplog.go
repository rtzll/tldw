package internal

import (
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/adrg/xdg"
)

var (
	mcpLogger     *log.Logger
	mcpLoggerOnce sync.Once
	mcpLogEnabled bool
)

// initMCPLogger initializes the MCP logger with file output
func initMCPLogger(enabled bool) {
	mcpLogEnabled = enabled

	if !enabled {
		return
	}

	// Create log directory
	logDir := filepath.Join(xdg.CacheHome, "tldw")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		// If we can't create the log directory, disable logging
		mcpLogEnabled = false
		return
	}

	// Open log file
	logPath := filepath.Join(logDir, "mcp.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// If we can't open the log file, disable logging
		mcpLogEnabled = false
		return
	}

	// Create logger with timestamp and microsecond precision
	mcpLogger = log.New(logFile, "", log.LstdFlags|log.Lmicroseconds)
}

// InitMCPLogging initializes MCP logging based on config
func InitMCPLogging(config *Config) {
	mcpLoggerOnce.Do(func() {
		initMCPLogger(config.MCPLogEnabled)
	})
}

// mcpLogf logs a formatted message if MCP logging is enabled
func mcpLogf(level, format string, args ...any) {
	if !mcpLogEnabled || mcpLogger == nil {
		return
	}

	mcpLogger.Printf("[MCP] [%s] "+format, append([]any{level}, args...)...)
}

// MCPLogInfo logs an info message
func MCPLogInfo(format string, args ...any) {
	mcpLogf("INFO", format, args...)
}

// MCPLogError logs an error message
func MCPLogError(format string, args ...any) {
	mcpLogf("ERROR", format, args...)
}

// MCPLogDebug logs a debug message
func MCPLogDebug(format string, args ...any) {
	mcpLogf("DEBUG", format, args...)
}
