package proxy

import (
	"context"
	"fmt"
	"net"
	"time"

	"go.uber.org/zap"
)

func (p *Proxy) handleConn(ctx context.Context, id uint64, clientConn net.Conn) {
	p.metrics.ActiveConnections.Inc()
	defer p.metrics.ActiveConnections.Dec()
	p.activeConn.Add(1)
	defer clientConn.Close()

	startTime := time.Now()
	log := p.logger.With(
		zap.Uint64("conn_id", id),
		zap.String("client", clientConn.RemoteAddr().String()),
	)

	log.Info("client connected",
		zap.Int64("active_connections", p.activeConn.Load()),
	)

	// Retry подключения к postgres: 3 попытки с backoff
	targetConn, err := p.dialPostgres(log)
	if err != nil {
		log.Error("postgres unavailable, closing client connection", zap.Error(err))
		p.activeConn.Add(-1)
		return
	}
	defer targetConn.Close()

	session := newSession(id, clientConn, targetConn, log, p.analyzer, p.policy, p.metrics, p.store, p.cfg.Log.LogSQL)
	if err := session.Run(ctx); err != nil {
		log.Debug("session ended", zap.Error(err))
	}

	p.analyzer.ForgetConn(id)
	p.activeConn.Add(-1)

	log.Info("client disconnected",
		zap.Duration("duration", time.Since(startTime)),
		zap.Int64("active_connections", p.activeConn.Load()),
	)
}

// dialPostgres пытается подключиться к postgres с экспоненциальным backoff
func (p *Proxy) dialPostgres(log *zap.Logger) (net.Conn, error) {
	delays := []time.Duration{0, 100 * time.Millisecond, 300 * time.Millisecond}

	for i, delay := range delays {
		if delay > 0 {
			log.Warn("retrying postgres connection",
				zap.Int("attempt", i+1),
				zap.Duration("delay", delay),
			)
			time.Sleep(delay)
		}

		conn, err := net.DialTimeout("tcp", p.cfg.Proxy.TargetAddr, 3*time.Second)
		if err == nil {
			return conn, nil
		}

		log.Debug("postgres connection attempt failed",
			zap.Int("attempt", i+1),
			zap.Error(err),
		)
	}

	return nil, fmt.Errorf("postgres unreachable at %s after %d attempts",
		p.cfg.Proxy.TargetAddr, len(delays))
}
