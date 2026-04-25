package analyzer

import (
	"fmt"

	pg_query "github.com/pganalyze/pg_query_go/v4"
)

// detectSelectStar ищет SELECT * в таргет-листе
func detectSelectStar(stmt *pg_query.SelectStmt) []Issue {
	for _, target := range stmt.TargetList {
		rt := target.GetResTarget()
		if rt == nil || rt.Val == nil {
			continue
		}
		cr := rt.Val.GetColumnRef()
		if cr == nil {
			continue
		}
		for _, field := range cr.Fields {
			if field.GetAStar() != nil {
				return []Issue{{
					Type:     IssueSelectStar,
					Severity: SeverityWarning,
					Message:  "SELECT * — specify columns explicitly to reduce network overhead",
				}}
			}
		}
	}
	return nil
}

// detectMissingLimit ищет SELECT без LIMIT на реальных таблицах
func detectMissingLimit(stmt *pg_query.SelectStmt) []Issue {
	// SELECT 1, SELECT now() и т.д. — FROM нет, LIMIT не нужен
	if len(stmt.FromClause) == 0 {
		return nil
	}

	// LIMIT уже есть
	if stmt.LimitCount != nil {
		return nil
	}

	// Агрегат без GROUP BY всегда возвращает 1 строку — LIMIT не нужен
	// Пример: SELECT count(*) FROM users
	if isSimpleAggregate(stmt) {
		return nil
	}

	return []Issue{{
		Type:     IssueMissingLimit,
		Severity: SeverityWarning,
		Message:  "SELECT without LIMIT can return unbounded rows",
	}}
}

// isSimpleAggregate — есть агрегатная функция и нет GROUP BY
func isSimpleAggregate(stmt *pg_query.SelectStmt) bool {
	if len(stmt.GroupClause) > 0 {
		return false
	}
	aggregates := map[string]bool{
		"count": true, "sum": true, "avg": true,
		"min": true, "max": true, "array_agg": true,
		"string_agg": true, "json_agg": true, "jsonb_agg": true,
	}
	for _, target := range stmt.TargetList {
		rt := target.GetResTarget()
		if rt == nil || rt.Val == nil {
			continue
		}
		fc := rt.Val.GetFuncCall()
		if fc == nil {
			continue
		}
		for _, fn := range fc.Funcname {
			s := fn.GetString_()
			if s != nil && aggregates[s.Sval] {
				return true
			}
		}
	}
	return false
}

// calcComplexity вычисляет score сложности запроса
func calcComplexity(stmt *pg_query.SelectStmt) int {
	score := 0

	// Каждый JOIN: +10
	for _, node := range stmt.FromClause {
		score += countJoins(node)
	}

	// Подзапросы в WHERE: +15 каждый
	if stmt.WhereClause != nil {
		score += countSubqueries(stmt.WhereClause) * 15
	}

	// GROUP BY: +3 за каждую колонку
	score += len(stmt.GroupClause) * 3

	// HAVING: +5
	if stmt.HavingClause != nil {
		score += 5
	}

	// ORDER BY: +2 за колонку
	score += len(stmt.SortClause) * 2

	// DISTINCT: +5
	if stmt.DistinctClause != nil {
		score += 5
	}

	return score
}

// countJoins рекурсивно считает JoinExpr
func countJoins(node *pg_query.Node) int {
	if node == nil {
		return 0
	}
	je := node.GetJoinExpr()
	if je == nil {
		return 0
	}
	// Сам JOIN + рекурсивно в обе стороны
	return 10 + countJoins(je.Larg) + countJoins(je.Rarg)
}

// countSubqueries считает подзапросы в узле
func countSubqueries(node *pg_query.Node) int {
	if node == nil {
		return 0
	}
	if node.GetSubLink() != nil {
		return 1
	}
	// BoolExpr (AND/OR): проверяем аргументы
	if be := node.GetBoolExpr(); be != nil {
		count := 0
		for _, arg := range be.Args {
			count += countSubqueries(arg)
		}
		return count
	}
	return 0
}

// complexityIssue создаёт Issue по score
func complexityIssue(score int) Issue {
	sev := SeverityWarning
	msg := fmt.Sprintf("complexity score %d — consider simplifying query", score)
	if score > 60 {
		sev = SeverityCritical
		msg = fmt.Sprintf("complexity score %d — query is very expensive", score)
	}
	return Issue{
		Type:     IssueHighComplexity,
		Severity: sev,
		Message:  msg,
	}
}
