package proxy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"queryguard/internal/analyzer"
	"queryguard/internal/dashboard"
	"queryguard/internal/metrics"
	"queryguard/internal/policy"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgproto3"
	"go.uber.org/zap"
)

// 1. В struct Session добавить поле:
type Session struct {
	id             uint64
	clientConn     net.Conn
	targetConn     net.Conn
	backend        *pgproto3.Backend
	frontend       *pgproto3.Frontend
	logger         *zap.Logger
	analyzer       *analyzer.Analyzer
	policy         *policy.Engine
	metrics        *metrics.Metrics
	store          *dashboard.Store
	backendMu      sync.Mutex
	timeMu         sync.Mutex
	queryStartTime time.Time
	lastVerdict    string
	lastProtocol   string
	pendingEntry   *dashboard.QueryEntry
}

// 2. Обновить сигнатуру newSession:
func newSession(
	id uint64,
	clientConn, targetConn net.Conn,
	logger *zap.Logger,
	az *analyzer.Analyzer,
	pe *policy.Engine,
	m *metrics.Metrics,
	store *dashboard.Store,
) *Session {
	return &Session{
		id:         id,
		clientConn: clientConn,
		targetConn: targetConn,
		logger:     logger,
		analyzer:   az,
		policy:     pe,
		metrics:    m,
		store:      store,
	}
}

func (s *Session) Run(ctx context.Context) error {
	// Фаза 1: сырые байты — никакого re-encoding
	if err := s.startup(); err != nil {
		return fmt.Errorf("startup: %w", err)
	}

	// Фаза 2: создаём pgproto3 ПОСЛЕ startup — буферы чистые
	s.backend = pgproto3.NewBackend(s.clientConn, s.clientConn)
	s.frontend = pgproto3.NewFrontend(s.targetConn, s.targetConn)

	s.logger.Info("session ready")
	return s.proxy(ctx)
}

// ─── STARTUP (сырые байты, без pgproto3) ────────────────────────────────────

func (s *Session) startup() error {
	// Читаем первое сообщение клиента
	raw, err := readStartupRaw(s.clientConn)
	if err != nil {
		return fmt.Errorf("read first message: %w", err)
	}

	// SSLRequest: длина=8, код=80877103
	if len(raw) == 8 && binary.BigEndian.Uint32(raw[4:]) == 80877103 {
		// Отказываем: 'N' = No SSL
		if _, err := s.clientConn.Write([]byte{'N'}); err != nil {
			return fmt.Errorf("decline SSL: %w", err)
		}
		s.logger.Debug("SSL declined")

		// Теперь читаем реальный StartupMessage
		raw, err = readStartupRaw(s.clientConn)
		if err != nil {
			return fmt.Errorf("read startup after SSL: %w", err)
		}
	}

	// Логируем параметры (raw[8:] = после length(4) + version(4))
	if len(raw) > 8 {
		params := parseStartupParams(raw[8:])
		s.logger.Info("client startup",
			zap.String("user", params["user"]),
			zap.String("database", params["database"]),
			zap.String("app", params["application_name"]),
		)
	}

	// Пересылаем СЫРЫЕ байты в postgres — ноль перекодировки
	if _, err := s.targetConn.Write(raw); err != nil {
		return fmt.Errorf("forward startup to postgres: %w", err)
	}

	// Пересылаем auth-обмен до ReadyForQuery
	return s.forwardAuth()
}

// readStartupRaw читает startup-сообщение (length + body, без type-байта)
func readStartupRaw(r io.Reader) ([]byte, error) {
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return nil, err
	}

	totalLen := int(binary.BigEndian.Uint32(lenBuf))
	if totalLen < 4 || totalLen > 65536 {
		return nil, fmt.Errorf("bad startup length: %d", totalLen)
	}

	body := make([]byte, totalLen-4)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}

	return append(lenBuf, body...), nil
}

// readMsgRaw читает обычное PostgreSQL-сообщение (type + length + body)
func readMsgRaw(r io.Reader) (msgType byte, full []byte, err error) {
	header := make([]byte, 5) // type(1) + length(4)
	if _, err = io.ReadFull(r, header); err != nil {
		return 0, nil, err
	}

	bodyLen := int(binary.BigEndian.Uint32(header[1:])) - 4
	if bodyLen < 0 {
		return 0, nil, fmt.Errorf("invalid message length")
	}

	body := make([]byte, bodyLen)
	if _, err = io.ReadFull(r, body); err != nil {
		return 0, nil, err
	}

	return header[0], append(header, body...), nil
}

// forwardAuth пересылает auth-сообщения до ReadyForQuery
func (s *Session) forwardAuth() error {
	for {
		msgType, full, err := readMsgRaw(s.targetConn)
		if err != nil {
			return fmt.Errorf("read postgres: %w", err)
		}

		// Пересылаем клиенту
		if _, err := s.clientConn.Write(full); err != nil {
			return fmt.Errorf("write to client: %w", err)
		}

		switch msgType {
		case 'Z': // ReadyForQuery — auth завершён
			s.logger.Debug("auth complete")
			return nil

		case 'E': // ErrorResponse
			return fmt.Errorf("postgres rejected connection")

		case 'R': // Authentication challenge
			if len(full) < 9 {
				return fmt.Errorf("auth message too short")
			}
			authType := binary.BigEndian.Uint32(full[5:9])

			// 0 = AuthOk, 12 = SASLFinal — ответ клиента не нужен
			if authType == 0 || authType == 12 {
				continue
			}

			// Для всех остальных (MD5=5, cleartext=3, SASL=10, SASLContinue=11)
			// читаем ответ клиента и пересылаем в postgres
			_, clientResp, err := readMsgRaw(s.clientConn)
			if err != nil {
				return fmt.Errorf("read client auth response: %w", err)
			}
			if _, err := s.targetConn.Write(clientResp); err != nil {
				return fmt.Errorf("forward auth response: %w", err)
			}
		}
	}
}

// parseStartupParams разбирает key\0value\0 пары из startup-сообщения
func parseStartupParams(data []byte) map[string]string {
	result := make(map[string]string)
	i := 0
	for i < len(data) && data[i] != 0 {
		j := i
		for j < len(data) && data[j] != 0 {
			j++
		}
		key := string(data[i:j])
		i = j + 1
		if i >= len(data) {
			break
		}
		j = i
		for j < len(data) && data[j] != 0 {
			j++
		}
		result[key] = string(data[i:j])
		i = j + 1
	}
	return result
}

// ─── PROXY (pgproto3, после startup) ────────────────────────────────────────

func (s *Session) proxy(ctx context.Context) error {
	errCh := make(chan error, 2)
	go func() { errCh <- s.clientToServer(ctx) }()
	go func() { errCh <- s.serverToClient(ctx) }()

	select {
	case err := <-errCh:
		if isNormalClose(err) {
			return nil
		}
		return err
	case <-ctx.Done():
		return nil
	}
}

// ─── THREAD-SAFE ОТПРАВКА КЛИЕНТУ ───────────────────────────────────────────

// sendToClient thread-safe отправка сообщений клиенту
func (s *Session) sendToClient(msgs ...pgproto3.BackendMessage) error {
	s.backendMu.Lock()
	defer s.backendMu.Unlock()
	for _, msg := range msgs {
		s.backend.Send(msg)
	}
	return s.backend.Flush()
}

// sendBlockResponse отправляет клиенту ошибку вместо результата запроса
// Postgres при этом ничего не получает — запрос до него не доходит
func (s *Session) sendBlockResponse(d *policy.Decision) {
	s.logger.Warn("BLOCKED",
		zap.String("policy", d.PolicyName),
		zap.String("message", d.Message),
	)

	//nolint:errcheck
	s.sendToClient(
		&pgproto3.ErrorResponse{
			Severity:            "ERROR",
			SeverityUnlocalized: "ERROR",
			Code:                "57014", // query_canceled
			Message:             fmt.Sprintf("QueryGuard: blocked by policy '%s'", d.PolicyName),
			Detail:              d.Message,
			Hint:                "Modify your query to comply with the database access policy",
		},
		&pgproto3.ReadyForQuery{TxStatus: 'I'},
	)
}

// ─── КЛИЕНТ СЕРВЕР ────────────────────────────────────────────────────────

func (s *Session) clientToServer(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		msg, err := s.backend.Receive()
		if err != nil {
			return err
		}

		// Анализируем SQL и выносим вердикт
		decision := s.analyzeAndDecide(msg)

		switch decision.Action {
		case policy.ActionBlock:
			s.sendBlockResponse(decision)
			continue // ждём следующий запрос

		case policy.ActionWarn:
			s.logger.Warn("policy warn",
				zap.String("policy", decision.PolicyName),
				zap.String("message", decision.Message),
			)
		}

		// ALLOW или WARN — пересылаем в postgres
		s.frontend.Send(msg)
		if err := s.frontend.Flush(); err != nil {
			return fmt.Errorf("flush to postgres: %w", err)
		}

		if _, ok := msg.(*pgproto3.Terminate); ok {
			return nil
		}
	}
}

func (s *Session) analyzeAndDecide(msg pgproto3.FrontendMessage) *policy.Decision {
	var sql, protocol string

	switch m := msg.(type) {
	case *pgproto3.Query:
		sql = m.String
		protocol = "simple"
	case *pgproto3.Parse:
		sql = m.Query
		protocol = "extended"
	default:
		return &policy.Decision{Action: policy.ActionAllow}
	}

	if sql == "" {
		return &policy.Decision{Action: policy.ActionAllow}
	}

	result := s.analyzer.Analyze(s.id, sql)
	decision := s.policy.Evaluate(result)

	verdict := strings.ToLower(string(decision.Action))

	// Fingerprint для читаемости
	fp := result.Fingerprint
	if len(fp) > 8 {
		fp = fp[:8]
	}

	s.logger.Info("⚡ SQL",
		zap.String("protocol", protocol),
		zap.String("sql", sql),
		zap.String("fingerprint", fp),
		zap.Int("complexity", result.Complexity),
		zap.String("verdict", string(decision.Action)),
	)

	//  Метрика: счётчик запросов
	s.metrics.QueriesTotal.WithLabelValues(verdict, protocol).Inc()

	// Метрика: найденные проблемы
	for _, issue := range result.Issues {
		s.logger.Warn(" issue",
			zap.String("type", string(issue.Type)),
			zap.String("severity", string(issue.Severity)),
			zap.String("message", issue.Message),
		)
		s.metrics.IssuesTotal.WithLabelValues(string(issue.Type)).Inc()
	}

	//  Метрика: заблокированные запросы
	if decision.Action == policy.ActionBlock && decision.PolicyName != "" {
		s.metrics.BlockedTotal.WithLabelValues(decision.PolicyName).Inc()
	}

	// Сохраняем время старта для замера duration
	// (только для запросов которые идут в postgres)
	if decision.Action != policy.ActionBlock {
		s.timeMu.Lock()
		s.queryStartTime = time.Now()
		s.lastVerdict = verdict
		s.lastProtocol = protocol
		s.timeMu.Unlock()
	}

	// Создаём запись для дашборда
	issueNames := make([]string, 0, len(result.Issues))
	for _, issue := range result.Issues {
		issueNames = append(issueNames, string(issue.Type))
	}

	entry := &dashboard.QueryEntry{
		Time:        time.Now(),
		ConnID:      s.id,
		Protocol:    protocol,
		SQL:         sql,
		Fingerprint: fp,
		Verdict:     verdict,
		PolicyName:  decision.PolicyName,
		Issues:      issueNames,
		Complexity:  result.Complexity,
	}

	if decision.Action == policy.ActionBlock {
		// Заблокированные — добавляем сразу (duration и rows = 0)
		s.store.Add(entry)
	} else {
		// Разрешённые — дождёмся CommandComplete для duration + rows
		s.timeMu.Lock()
		s.pendingEntry = entry
		s.timeMu.Unlock()
	}

	return decision
}

// ─── СЕРВЕР → КЛИЕНТ ────────────────────────────────────────────────────────

func (s *Session) serverToClient(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		msg, err := s.frontend.Receive()
		if err != nil {
			return err
		}

		s.interceptServerMessage(msg)

		// Используем sendToClient — защита от race с clientToServer
		if err := s.sendToClient(msg); err != nil {
			return fmt.Errorf("flush to client: %w", err)
		}
	}
}

// ─── ИНСПЕКЦИЯ СЕРВЕРНЫХ СООБЩЕНИЙ ──────────────────────────────────────────

func (s *Session) interceptServerMessage(msg pgproto3.BackendMessage) {
	switch m := msg.(type) {
	case *pgproto3.RowDescription:
		cols := make([]string, len(m.Fields))
		for i, f := range m.Fields {
			cols[i] = string(f.Name)
		}
		s.logger.Debug("columns", zap.Strings("fields", cols))

	case *pgproto3.CommandComplete:
		tag := string(m.CommandTag)
		s.logger.Info("done", zap.String("tag", tag))

		s.timeMu.Lock()
		entry := s.pendingEntry
		start := s.queryStartTime
		verdict := s.lastVerdict
		s.pendingEntry = nil
		s.queryStartTime = time.Time{}
		s.timeMu.Unlock()

		if !start.IsZero() {
			dur := time.Since(start)

			// Метрики
			s.metrics.QueryDuration.WithLabelValues(verdict).Observe(dur.Seconds())
			rows := metrics.ParseRowCount(tag)
			if rows > 0 {
				s.metrics.RowsReturned.WithLabelValues(verdict).Observe(rows)
			}

			// Дашборд — дополняем запись и сохраняем
			if entry != nil {
				entry.DurationMs = float64(dur.Milliseconds())
				entry.Rows = int64(rows)
				s.store.Add(entry)
			}
		}

	case *pgproto3.ReadyForQuery:
		s.logger.Debug("ready", zap.String("tx", txStatus(m.TxStatus)))

	case *pgproto3.ErrorResponse:
		s.logger.Warn("postgres error",
			zap.String("severity", m.Severity),
			zap.String("code", m.Code),
			zap.String("message", m.Message),
		)
	}
}

// ─── ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ ────────────────────────────────────────────────

func txStatus(b byte) string {
	switch b {
	case 'I':
		return "idle"
	case 'T':
		return "in_transaction"
	case 'E':
		return "error"
	default:
		return string([]byte{b})
	}
}

func isNormalClose(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr *net.OpError
	return errors.As(err, &netErr)
}
