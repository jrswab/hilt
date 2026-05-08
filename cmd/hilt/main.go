package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"log/slog"

	"github.com/jrswab/hilt/internal/config"
)

const version = "0.1.0"

// resolveLogLevel maps a string to a slog.Level. Unknown values fall back to info.
func resolveLogLevel(input string) slog.Level {
	switch input {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// resolveConfigPath returns the flag value if non-empty, otherwise checks
// the HILT_CONFIG environment variable.
func resolveConfigPath(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return os.Getenv("HILT_CONFIG")
}

func main() {
	configFlag := flag.String("config", "", "path to config file")
	logLevelFlag := flag.String("log-level", "info", "log level: debug, info, warn, error")
	flag.Parse()

	level := resolveLogLevel(*logLevelFlag)
	if *logLevelFlag != "debug" && *logLevelFlag != "info" && *logLevelFlag != "warn" && *logLevelFlag != "error" {
		fmt.Fprintf(os.Stderr, "warning: invalid log level %q, falling back to info\n", *logLevelFlag)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	configPath := resolveConfigPath(*configFlag)

	logger.Info("hilt starting",
		slog.String("version", version),
		slog.String("config_path", configPath),
	)
	logger.Debug("resolved config", slog.String("path", configPath))

	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Error("failed to load config", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("config loaded",
		slog.String("path", configPath),
		slog.String("workspace_dir", cfg.WorkspaceDir),
	)

	// cfg is available for downstream use (e.g., Telegram bot, session manager).
	_ = cfg

	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()

	// TODO: initialise Telegram bot, session manager, etc.

	_ = ctx // placate compiler until server loop is wired in

	logger.Info("waiting for signals...")
	logger.Info("server not yet implemented")
	os.Exit(0)
}
