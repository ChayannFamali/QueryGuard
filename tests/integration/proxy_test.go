package integration_test

import (
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const proxyDSN = "postgres://postgres:postgres@localhost:5433/postgres?sslmode=disable"

func TestProxy_BlocksMissingLimit(t *testing.T) {
	db, err := sql.Open("pgx", proxyDSN)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	_, err = db.Exec("SELECT * FROM users")
	if err == nil {
		t.Fatal("expected block, got nil")
	}
	t.Logf("correctly blocked: %v", err)
}

func TestProxy_AllowsCountQuery(t *testing.T) {
	db, err := sql.Open("pgx", proxyDSN)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var count int
	err = db.QueryRow("SELECT count(*) FROM users").Scan(&count)
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 1000 {
		t.Fatalf("expected 1000, got %d", count)
	}
	t.Logf("count = %d ", count)
}

func TestProxy_AllowsSelectWithLimit(t *testing.T) {
	db, err := sql.Open("pgx", proxyDSN)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT id, name FROM users LIMIT 5")
	if err != nil {
		t.Fatalf("select failed: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
	}
	if count != 5 {
		t.Fatalf("expected 5 rows, got %d", count)
	}
	t.Logf("got %d rows ", count)
}

func TestProxy_TransactionsWork(t *testing.T) {
	db, err := sql.Open("pgx", proxyDSN)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	var count int
	tx.QueryRow("SELECT count(*) FROM users LIMIT 1").Scan(&count)

	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	t.Log("transaction ok")
}
