package proxy

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"

	"queryguard/internal/analyzer"
	"queryguard/internal/config"
	"queryguard/internal/dashboard"
	"queryguard/internal/metrics"
	"queryguard/internal/policy"
)

type Proxy struct {
	cfg        *config.Config
	logger     *zap.Logger
	activeConn atomic.Int64
	connID     atomic.Uint64
	analyzer   *analyzer.Analyzer
	policy     *policy.Engine
	metrics    *metrics.Metrics
	store      *dashboard.Store // ← новое
}

func New(cfg *config.Config, logger *zap.Logger) (*Proxy, error) {
	az := analyzer.New()
	pe, err := policy.New(cfg.Policy.ConfigPath, cfg.Policy.DryRun, logger)
	if err != nil {
		return nil, fmt.Errorf("init policy engine: %w", err)
	}

	return &Proxy{
		cfg:      cfg,
		logger:   logger,
		analyzer: az,
		policy:   pe,
		metrics:  metrics.New(),
		store:    dashboard.NewStore(), // ← новое
	}, nil
}

func (p *Proxy) Store() *dashboard.Store { return p.store }

// Добавь эти два метода рядом с методом Start()
func (p *Proxy) Metrics() *metrics.Metrics {
	return p.metrics
}

// Start — без изменений (оставь как было)
func (p *Proxy) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", p.cfg.Proxy.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", p.cfg.Proxy.ListenAddr, err)
	}
	defer listener.Close()

	p.logger.Info("proxy listening",
		zap.String("addr", p.cfg.Proxy.ListenAddr),
		zap.String("forwarding_to", p.cfg.Proxy.TargetAddr),
		zap.Bool("dry_run", p.policy.IsDryRun()),
	)

	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	var wg sync.WaitGroup

	for {
		clientConn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				p.logger.Info("proxy: shutting down",
					zap.Int64("active", p.activeConn.Load()),
				)
				wg.Wait()
				return nil
			default:
				p.logger.Error("accept failed", zap.Error(err))
				continue
			}
		}

		id := p.connID.Add(1)
		wg.Add(1)
		go func(id uint64, conn net.Conn) {
			defer wg.Done()
			p.handleConn(ctx, id, conn)
		}(id, clientConn)
	}
}
