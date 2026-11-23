package mcpserver

import (
	"io"
	"log"
	"strings"
)

// Logger belongs to one server. The caller owns its output file.
type Logger struct{ log *log.Logger }

func NewLogger(output io.Writer) *Logger {
	return &Logger{log.New(output, "", log.LstdFlags|log.Lmicroseconds)}
}

func (l *Logger) Printf(format string, args ...any) {
	l.log.Printf("[MCP] [INFO] "+strings.TrimSuffix(format, "\n"), args...)
}

func (l *Logger) errorf(format string, args ...any) {
	l.log.Printf("[MCP] [ERROR] "+format, args...)
}
