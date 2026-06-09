package analyzer

import (
	"context"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v4"
)

// Options holds configurable analyzer thresholds
type Options struct {
	N1Threshold    int
	ComplexityWarn int
	ComplexityCrit int
}

type Analyzer struct {
	n1  *N1Detector
	opts Options
}

func New(ctx context.Context, opts Options) *Analyzer {
	return &Analyzer{
		n1:   newN1Detector(ctx, opts.N1Threshold),
		opts: opts,
	}
}

func (a *Analyzer) Analyze(connID uint64, sql string) *Result {
	result := &Result{SQL: sql}

	// Fingerprint — работает даже если парсинг упадёт
	if fp, err := pg_query.Fingerprint(sql); err == nil {
		result.Fingerprint = fp
	}

	// Нормализация — заменяет литералы на $1, $2...
	if norm, err := pg_query.Normalize(sql); err == nil {
		result.Normalized = norm
	}

	// N+1 детектор (по fingerprint)
	if result.Fingerprint != "" && a.n1.Record(connID, result.Fingerprint) {
		result.Issues = append(result.Issues, Issue{
			Type:     IssueN1,
			Severity: SeverityWarning,
			Message:  "possible N+1: same query pattern executed many times in 1 second",
		})
	}

	// Fast-path: skip AST parsing for non-SELECT statements
	trimmed := strings.TrimSpace(sql)
	if len(trimmed) < 6 {
		return result
	}
	head := trimmed[:min(10, len(trimmed))]
	if !strings.EqualFold(head, "select") && !strings.HasPrefix(strings.ToUpper(head), "SELECT") {
		return result
	}

	// Парсим AST
	parsed, err := pg_query.Parse(sql)
	if err != nil {
		result.ParseError = err
		return result // не можем анализировать дальше — не страшно
	}

	for _, stmt := range parsed.Stmts {
		if stmt.Stmt == nil {
			continue
		}
		sel := stmt.Stmt.GetSelectStmt()
		if sel == nil {
			continue // INSERT, UPDATE, DELETE — пока не анализируем
		}

		result.Issues = append(result.Issues, detectSelectStar(sel)...)
		result.Issues = append(result.Issues, detectMissingLimit(sel)...)
		result.Complexity += calcComplexity(sel)
	}

	// Complexity issue using configurable thresholds
	warnThreshold := a.opts.ComplexityWarn
	critThreshold := a.opts.ComplexityCrit
	if warnThreshold <= 0 {
		warnThreshold = 30
	}
	if critThreshold <= 0 {
		critThreshold = 60
	}
	if result.Complexity >= warnThreshold {
		result.Issues = append(result.Issues, complexityIssue(result.Complexity, warnThreshold, critThreshold))
	}

	return result
}

// Stop shuts down background goroutines (N+1 cleanup loop)
func (a *Analyzer) Stop() {
	a.n1.Stop()
}

// ForgetConn вызывается при дисконнекте клиента
func (a *Analyzer) ForgetConn(connID uint64) {
	a.n1.ForgetConn(connID)
}
