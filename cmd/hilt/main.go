package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"log/slog"

	"github.com/jrswab/axe/pkg/runner"
	"github.com/jrswab/hilt/internal/config"
	"github.com/jrswab/hilt/internal/mainagent"
	"github.com/jrswab/hilt/internal/memory"
	"github.com/jrswab/hilt/internal/server"
	"github.com/jrswab/hilt/internal/session"
	"github.com/jrswab/hilt/internal/telegram"
)

const version = "0.1.0"

// axeRunner wraps runner.Run to satisfy the mainagent.Runner interface.
type axeRunner struct{}

func (a *axeRunner) Run(ctx context.Context, opts runner.Options) (*runner.Result, error) {
	return runner.Run(ctx, opts)
}

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

// startSessionManager initialises the session manager, ensures an active
// session exists, archives stale sessions, and prunes old archives.
func startSessionManager(ctx context.Context, dbPath string, cfg *config.Config, logger *slog.Logger) (*session.Manager, *session.Session, error) {
	mgr, err := session.NewManager(dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("creating session manager: %w", err)
	}

	activeSession, err := mgr.GetActiveSession(ctx)
	if err != nil {
		_ = mgr.Close()
		return nil, nil, fmt.Errorf("getting active session: %w", err)
	}

	if cfg.SessionTTLDays > 0 {
		ttl := time.Duration(cfg.SessionTTLDays) * 24 * time.Hour
		if time.Since(activeSession.LastActivity) > ttl {
			logger.Info("archiving stale session", slog.Int64("session_id", activeSession.ID))
			if err := mgr.ArchiveSession(ctx, activeSession.ID); err != nil {
				_ = mgr.Close()
				return nil, nil, fmt.Errorf("archiving stale session: %w", err)
			}
			activeSession, err = mgr.CreateSession(ctx)
			if err != nil {
				_ = mgr.Close()
				return nil, nil, fmt.Errorf("creating new session: %w", err)
			}
		}
	}

	if cfg.SessionTTLDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -cfg.SessionTTLDays)
		if _, err := mgr.PruneOldSessions(ctx, cutoff); err != nil {
			_ = mgr.Close()
			return nil, nil, fmt.Errorf("pruning old sessions: %w", err)
		}
	}

	logger.Info("active session loaded", slog.Int64("session_id", activeSession.ID))
	return mgr, activeSession, nil
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

	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()

	dbDir, err := config.ExpandPath("~/.config/hilt")
	if err != nil {
		logger.Error("failed to resolve db directory", slog.String("error", err.Error()))
		os.Exit(1)
	}
	dbPath := filepath.Join(dbDir, "hilt.sqlite")

	mgr, activeSession, err := startSessionManager(ctx, dbPath, cfg, logger)
	if err != nil {
		logger.Error("failed to start session manager", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer mgr.Close()

	bot, err := telegram.NewBot(cfg.TelegramBotToken, cfg.AllowedUserIDs)
	if err != nil {
		logger.Error("failed to initialize telegram bot", slog.String("error", err.Error()))
		os.Exit(1)
	}

	memoryReader := memory.NewReader(cfg.WorkspaceDir)
	agentsDir := filepath.Join(dbDir, "agents")
	historyBuilder := mainagent.NewHistoryBuilder(mgr)
	processor := mainagent.NewProcessor(
		mgr,            // ActiveSessionProvider
		mgr,            // TurnStore
		mgr,            // SessionStore
		memoryReader,   // FileReader
		&axeRunner{},   // Runner
		bot,            // Messenger
		agentsDir,
		cfg.MainAgentModel,
		logger,
		historyBuilder,
		&mainagent.AxeErrorMapper{},
	)

	router := server.NewRouter(bot, mgr, processor, cfg.SessionTTLDays, logger)

	logger.Info("hilt ready", slog.Int64("active_session_id", activeSession.ID))
	logger.Info("starting telegram polling")

	if err := bot.Start(ctx, router.HandleMessage); err != nil {
		logger.Error("telegram polling error", slog.String("error", err.Error()))
	}
	logger.Info("shutting down")
}
