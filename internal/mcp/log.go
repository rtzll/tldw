package mcpserver

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

// InitLogging initializes MCP logging.
func InitLogging(enabled bool) {
	mcpLoggerOnce.Do(func() {
		mcpLogEnabled = enabled
		if !enabled {
			return
		}

		logDir := filepath.Join(xdg.CacheHome, "tldw")
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			mcpLogEnabled = false
			return
		}

		logPath := filepath.Join(logDir, "mcp.log")
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			mcpLogEnabled = false
			return
		}

		mcpLogger = log.New(logFile, "", log.LstdFlags|log.Lmicroseconds)
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
