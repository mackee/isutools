package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
)

var logLevelMap = map[string]slog.Level{
	"debug": slog.LevelDebug,
	"info":  slog.LevelInfo,
	"warn":  slog.LevelWarn,
	"error": slog.LevelError,
}

func main() {
	ctx := context.Background()
	var fix bool
	var logLevelStr string
	flag.BoolVar(&fix, "fix", false, "fix the code")
	flag.StringVar(&logLevelStr, "log-level", "info", "log level")
	flag.Parse()

	logLevel, ok := logLevelMap[logLevelStr]
	if !ok {
		logLevel = slog.LevelInfo
	}
	if err := Run(ctx, "./", &Opts{Fix: fix, LogLevel: logLevel}); err != nil {
		slog.ErrorContext(ctx, "error occurred", slog.Any("error", err))
		os.Exit(1)
	}
}
