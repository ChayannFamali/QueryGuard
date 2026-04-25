package metrics

import (
	"context"
	"net/http"
	"time"

	"go.uber.org/zap"
)

func StartServer(ctx context.Context, addr string, m *Metrics, logger *zap.Logger) error {
	mux := http.NewServeMux()

	// Prometheus scrape endpoint
	mux.Handle("/metrics", m.Handler())

	// Kubernetes probes
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Shutdown при отмене контекста
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	logger.Info("metrics server listening",
		zap.String("addr", addr),
		zap.String("metrics_url", "http://"+addr+"/metrics"),
	)

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
