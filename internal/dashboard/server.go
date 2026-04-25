package dashboard

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

//go:embed templates/*.html
var templateFS embed.FS

type Server struct {
	store   *Store
	logger  *zap.Logger
	dryRun  bool
	version string
	tmpl    *template.Template
}

func NewServer(store *Store, dryRun bool, logger *zap.Logger) (*Server, error) {
	funcMap := template.FuncMap{
		"formatTime": func(t time.Time) string {
			return t.Format("15:04:05.000")
		},
		"formatDur": func(ms float64) string {
			if ms == 0 {
				return "—"
			}
			return fmt.Sprintf("%.1fms", ms)
		},
		"durClass": func(ms float64) string {
			switch {
			case ms > 500:
				return "slow"
			case ms > 100:
				return "medium"
			default:
				return ""
			}
		},
		"truncate": func(s string, n int) string {
			if len(s) <= n {
				return s
			}
			return s[:n] + "…"
		},
		"joinIssues": func(issues []string) string {
			return strings.Join(issues, ", ")
		},
	}

	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	return &Server{
		store:   store,
		logger:  logger,
		dryRun:  dryRun,
		version: "0.0.1",
		tmpl:    tmpl,
	}, nil
}

func (s *Server) Start(ctx context.Context, addr string) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/events", s.handleSSE)
	mux.HandleFunc("/partial/stats", s.handleStatsPartial)

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	s.logger.Info("dashboard listening",
		zap.String("addr", addr),
		zap.String("url", "http://"+addr),
	)

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// ─── Handlers ───────────────────────────────────────────────────────────────

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	stats := s.store.Stats()
	entries := s.store.Recent(100)

	data := map[string]any{
		"Version": s.version,
		"DryRun":  s.dryRun,
		"Stats":   stats,
		"Entries": entries,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		s.logger.Error("render index", zap.Error(err))
	}
}

func (s *Server) handleStatsPartial(w http.ResponseWriter, r *http.Request) {
	stats := s.store.Stats()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "stats", stats); err != nil {
		s.logger.Error("render stats", zap.Error(err))
	}
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // для nginx

	// Ping чтобы соединение не закрылось
	fmt.Fprintf(w, ": ping\n\n")
	flusher.Flush()

	subID, ch := s.store.Subscribe()
	defer s.store.Unsubscribe(subID)

	ticker := time.NewTicker(15 * time.Second) // keepalive
	defer ticker.Stop()

	for {
		select {
		case entry, ok := <-ch:
			if !ok {
				return
			}

			// Рендерим строку таблицы в HTML
			html, err := s.renderRow(entry)
			if err != nil {
				s.logger.Error("render row", zap.Error(err))
				continue
			}

			// SSE формат: event + data (одна строка)
			fmt.Fprintf(w, "event: query\ndata: %s\n\n",
				strings.ReplaceAll(html, "\n", ""),
			)
			flusher.Flush()

		case <-ticker.C:
			// Keepalive comment
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()

		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) renderRow(entry *QueryEntry) (string, error) {
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, "row", entry); err != nil {
		return "", err
	}
	return buf.String(), nil
}
