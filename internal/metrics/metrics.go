package metrics

import (
	"strconv"
	"strings"

	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	// Сколько запросов прошло — по вердикту и протоколу
	QueriesTotal *prometheus.CounterVec

	// Сколько запросов заблокировано — по имени политики
	BlockedTotal *prometheus.CounterVec

	// Сколько проблем найдено — по типу
	IssuesTotal *prometheus.CounterVec

	// Сколько строк вернул запрос
	RowsReturned *prometheus.HistogramVec

	// Время выполнения запроса (от прокси до ответа postgres)
	QueryDuration *prometheus.HistogramVec

	// Активные соединения прямо сейчас
	ActiveConnections prometheus.Gauge

	registry *prometheus.Registry
}

func New() *Metrics {
	reg := prometheus.NewRegistry()

	// Стандартные метрики Go runtime и процесса
	reg.MustRegister(prometheus.NewGoCollector())
	reg.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))

	f := promauto.With(reg)

	return &Metrics{
		registry: reg,

		QueriesTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "queryguard_queries_total",
			Help: "Total queries processed by verdict and protocol",
		}, []string{"verdict", "protocol"}),

		BlockedTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "queryguard_blocked_queries_total",
			Help: "Total blocked queries by policy name",
		}, []string{"policy"}),

		IssuesTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "queryguard_issues_detected_total",
			Help: "Total issues detected by type",
		}, []string{"issue_type"}),

		RowsReturned: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "queryguard_rows_returned",
			Help:    "Number of rows returned per query",
			Buckets: []float64{0, 1, 10, 100, 500, 1000, 5000, 10000, 100000},
		}, []string{"verdict"}),

		QueryDuration: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "queryguard_query_duration_seconds",
			Help:    "Query duration from proxy perspective (send to postgres → CommandComplete)",
			Buckets: []float64{0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		}, []string{"verdict"}),

		ActiveConnections: f.NewGauge(prometheus.GaugeOpts{
			Name: "queryguard_active_connections",
			Help: "Number of active client connections right now",
		}),
	}
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// ParseRowCount извлекает количество строк из CommandComplete тега
// "SELECT 42" → 42,  "INSERT 0 1" → 1,  "UPDATE 5" → 5
func ParseRowCount(tag string) float64 {
	parts := strings.Fields(tag)
	if len(parts) == 0 {
		return 0
	}
	n, err := strconv.ParseFloat(parts[len(parts)-1], 64)
	if err != nil {
		return 0
	}
	return n
}
