package analyzer

import (
	pg_query "github.com/pganalyze/pg_query_go/v4"
)

type Analyzer struct {
	n1 *N1Detector
}

func New() *Analyzer {
	return &Analyzer{
		n1: newN1Detector(),
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

	// Complexity issue
	if result.Complexity > 30 {
		result.Issues = append(result.Issues, complexityIssue(result.Complexity))
	}

	return result
}

// ForgetConn вызывается при дисконнекте клиента
func (a *Analyzer) ForgetConn(connID uint64) {
	a.n1.ForgetConn(connID)
}
