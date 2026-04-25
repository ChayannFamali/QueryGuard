package analyzer

type Severity string

const (
	SeverityInfo     Severity = "INFO"
	SeverityWarning  Severity = "WARNING"
	SeverityCritical Severity = "CRITICAL"
)

type IssueType string

const (
	IssueSelectStar     IssueType = "SELECT_STAR"
	IssueMissingLimit   IssueType = "MISSING_LIMIT"
	IssueHighComplexity IssueType = "HIGH_COMPLEXITY"
	IssueN1             IssueType = "N_PLUS_ONE"
)

type Issue struct {
	Type     IssueType
	Severity Severity
	Message  string
}

type Result struct {
	SQL         string
	Normalized  string // SELECT * FROM t WHERE id = $1
	Fingerprint string // хэш нормализованного запроса
	Complexity  int    // score сложности
	Issues      []Issue
	ParseError  error
}

func (r *Result) HasIssues() bool {
	return len(r.Issues) > 0
}

func (r *Result) HighestSeverity() Severity {
	for _, issue := range r.Issues {
		if issue.Severity == SeverityCritical {
			return SeverityCritical
		}
	}
	for _, issue := range r.Issues {
		if issue.Severity == SeverityWarning {
			return SeverityWarning
		}
	}
	return SeverityInfo
}
