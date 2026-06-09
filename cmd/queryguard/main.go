package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"queryguard/internal/config"
	"queryguard/internal/dashboard"
	"queryguard/internal/metrics"
	"queryguard/internal/proxy"
)

var version = "0.0.1"

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to config file")
	flag.Parse()

	fmt.Printf("QueryGuard v%s starting...\n", version)

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger, err := buildLogger(cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to init logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	// Stacktrace только для ERROR и выше
	logger = logger.WithOptions(zap.AddStacktrace(zap.ErrorLevel))

	logger.Info("config loaded",
		zap.String("listen_addr", cfg.Proxy.ListenAddr),
		zap.String("target_addr", cfg.Proxy.TargetAddr),
		zap.Bool("dry_run", cfg.Policy.DryRun),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	p, err := proxy.New(ctx, cfg, logger)
	if err != nil {
		logger.Error("failed to init proxy", zap.Error(err))
		os.Exit(1)
	}
	defer p.Analyzer().Stop()

	// Metrics HTTP сервер
	if cfg.Metrics.Enabled {
		go func() {
			if err := metrics.StartServer(ctx, cfg.Metrics.ListenAddr, p.Metrics(), cfg.Metrics.Username, cfg.Metrics.Password, logger); err != nil {
				logger.Error("metrics server error", zap.Error(err))
			}
		}()
	}
	if cfg.Dashboard.Enabled {
		dash, err := dashboard.NewServer(p.Store(), cfg.Policy.DryRun, cfg.Dashboard.Username, cfg.Dashboard.Password, logger)
		if err != nil {
			logger.Error("failed to init dashboard", zap.Error(err))
			os.Exit(1)
		}
		go func() {
			if err := dash.Start(ctx, cfg.Dashboard.ListenAddr); err != nil {
				logger.Error("dashboard error", zap.Error(err))
			}
		}()
	}
	if err := p.Start(ctx); err != nil {
		logger.Error("proxy error", zap.Error(err))
		os.Exit(1)
	}

	logger.Info("QueryGuard stopped. Bye!")
}

func buildLogger(level, format string) (*zap.Logger, error) {
	var zapCfg zap.Config

	if format == "json" {
		zapCfg = zap.NewProductionConfig()
	} else {
		zapCfg = zap.NewDevelopmentConfig()
	}

	switch level {
	case "debug":
		zapCfg.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	case "warn":
		zapCfg.Level = zap.NewAtomicLevelAt(zap.WarnLevel)
	case "error":
		zapCfg.Level = zap.NewAtomicLevelAt(zap.ErrorLevel)
	default:
		zapCfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	return zapCfg.Build()
}
