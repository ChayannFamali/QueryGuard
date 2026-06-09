package metrics

import (
	"context"
	"crypto/subtle"
	"net/http"
	"time"

	"go.uber.org/zap"
)

func StartServer(ctx context.Context, addr string, m *Metrics, username, password string, logger *zap.Logger) error {
	mux := http.NewServeMux()

	// Prometheus scrape endpoint (auth-protected when credentials configured)
	mux.Handle("/metrics", m.Handler())

	// Kubernetes probes — always unauthenticated
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	var handler http.Handler = mux
	if username != "" && password != "" {
		handler = basicAuthMiddleware(mux, username, password)
	}

	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
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
		zap.Bool("auth_enabled", username != ""),
	)

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func basicAuthMiddleware(next http.Handler, username, password string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow health probes without auth (needed by K8s)
		if r.URL.Path == "/health" || r.URL.Path == "/ready" {
			next.ServeHTTP(w, r)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok ||
			subtle.ConstantTimeCompare([]byte(user), []byte(username)) != 1 ||
			subtle.ConstantTimeCompare([]byte(pass), []byte(password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="QueryGuard Metrics"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
