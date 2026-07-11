// Package store открывает единственный SQLite-хендл гейта. Поверх него живут два независимых
// набора таблиц: настройки баз 1С (internal/tenant) и OAuth-клиенты/коды/токены (internal/oauth).
// Файл один, чтобы бэкап был одним артефактом, а миграции — каждая своя.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Open создаёт директорию под файл БД (если надо) и открывает соединение.
// Один writer — чтобы не ловить "database is locked" на SQLite WAL под нагрузкой.
func Open(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("store: ensure dir: %w", err)
		}
	}

	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open db: %w", err)
	}
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping db: %w", err)
	}
	return db, nil
}
