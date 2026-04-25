package policy

import (
	"fmt"
	"os"
	"sync"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"queryguard/internal/analyzer"
)

type Action string

const (
	ActionAllow Action = "ALLOW"
	ActionWarn  Action = "WARN"
	ActionBlock Action = "BLOCK"
)

type Policy struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	On          []string `yaml:"on"` // список IssueType
	Action      Action   `yaml:"action"`
	Message     string   `yaml:"message"`
}

type policiesFile struct {
	Policies []Policy `yaml:"policies"`
}

// Decision — итоговый вердикт по запросу
type Decision struct {
	Action     Action
	PolicyName string
	Message    string
}

var decisionAllow = &Decision{Action: ActionAllow}

type Engine struct {
	mu       sync.RWMutex
	policies []Policy
	dryRun   bool
	logger   *zap.Logger
}

func New(configPath string, dryRun bool, logger *zap.Logger) (*Engine, error) {
	e := &Engine{
		dryRun: dryRun,
		logger: logger,
	}
	if err := e.Load(configPath); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *Engine) Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open policies: %w", err)
	}
	defer f.Close()

	var pf policiesFile
	if err := yaml.NewDecoder(f).Decode(&pf); err != nil {
		return fmt.Errorf("decode policies: %w", err)
	}

	e.mu.Lock()
	e.policies = pf.Policies
	e.mu.Unlock()

	e.logger.Info("policies loaded", zap.Int("count", len(pf.Policies)))
	return nil
}

// Evaluate прогоняет результат анализа через все политики
// Приоритет: BLOCK > WARN > ALLOW
func (e *Engine) Evaluate(result *analyzer.Result) *Decision {
	if result == nil || len(result.Issues) == 0 {
		return decisionAllow
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	// Быстрый lookup: какие типы проблем есть в запросе
	issueSet := make(map[string]bool, len(result.Issues))
	for _, issue := range result.Issues {
		issueSet[string(issue.Type)] = true
	}

	var warnDecision *Decision // запоминаем WARN если найдём

	for i := range e.policies {
		p := &e.policies[i]
		for _, issueType := range p.On {
			if !issueSet[issueType] {
				continue
			}

			switch p.Action {
			case ActionBlock:
				action := ActionBlock
				if e.dryRun {
					// dry-run: блокировка превращается в предупреждение
					action = ActionWarn
					e.logger.Debug("dry-run: BLOCK → WARN", zap.String("policy", p.Name))
				}
				return &Decision{
					Action:     action,
					PolicyName: p.Name,
					Message:    p.Message,
				}

			case ActionWarn:
				if warnDecision == nil {
					warnDecision = &Decision{
						Action:     ActionWarn,
						PolicyName: p.Name,
						Message:    p.Message,
					}
				}
			}
		}
	}

	if warnDecision != nil {
		return warnDecision
	}
	return decisionAllow
}

func (e *Engine) IsDryRun() bool {
	return e.dryRun
}
