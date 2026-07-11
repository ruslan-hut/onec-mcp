package tenant

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrNotFound — базы с таким слагом нет.
	ErrNotFound = errors.New("tenant: not found")
	// ErrExists — слаг занят. Отдаётся наверх, чтобы /admin показал внятное сообщение вместо 500.
	ErrExists = errors.New("tenant: slug already exists")
)

// Store — CRUD над настройками баз. Работает на общем *sql.DB (см. internal/store).
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) (*Store, error) {
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tenants (
			slug TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			base_url TEXT NOT NULL,
			username TEXT NOT NULL DEFAULT '',
			password TEXT NOT NULL DEFAULT '',
			timeout_ms INTEGER NOT NULL DEFAULT 8000,
			report_timeout_ms INTEGER NOT NULL DEFAULT 45000,
			resolve_cache_ttl_sec INTEGER NOT NULL DEFAULT 600,
			tenant_header TEXT NOT NULL DEFAULT '',
			default_tenant TEXT NOT NULL DEFAULT '',
			mcp_token TEXT NOT NULL DEFAULT '',
			api_token TEXT NOT NULL DEFAULT '',
			dev_access_key TEXT NOT NULL DEFAULT '',
			default_scopes TEXT NOT NULL DEFAULT '[]',
			supported_scopes TEXT NOT NULL DEFAULT '[]',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("tenant: migrate: %w", err)
		}
	}
	return nil
}

const tenantColumns = `slug, name, enabled, base_url, username, password,
	timeout_ms, report_timeout_ms, resolve_cache_ttl_sec, tenant_header, default_tenant,
	mcp_token, api_token, dev_access_key, default_scopes, supported_scopes, created_at, updated_at`

// List — все базы, включая выключенные, в порядке слага (детерминированный вывод в /admin и логах).
func (s *Store) List(ctx context.Context) ([]*Tenant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+tenantColumns+` FROM tenants ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []*Tenant
	for rows.Next() {
		t, err := scanTenant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) Get(ctx context.Context, slug string) (*Tenant, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+tenantColumns+` FROM tenants WHERE slug = ?`, slug)
	t, err := scanTenant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

// Create — вставка новой базы. Слаг занят → ErrExists.
func (s *Store) Create(ctx context.Context, t *Tenant) error {
	t.Normalize()
	if err := t.Validate(); err != nil {
		return err
	}

	now := time.Now()
	t.CreatedAt, t.UpdatedAt = now, now

	defaults, supported, err := marshalScopes(t)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO tenants (`+tenantColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Slug, t.Name, boolToInt(t.Enabled), t.BaseURL, t.Username, t.Password,
		t.TimeoutMs, t.ReportTimeoutMs, t.ResolveCacheTTLSec, t.TenantHeader, t.DefaultTenant,
		t.MCPToken, t.APIToken, t.DevAccessKey, defaults, supported,
		t.CreatedAt.Unix(), t.UpdatedAt.Unix(),
	)
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return ErrExists
	}
	return err
}

// Update — полная перезапись записи по слагу. Слаг не меняется: он в URL коннекторов,
// в issuer и в привязке выпущенных токенов — переименование сломало бы всё это молча.
// Нужен другой слаг — заводится новая база.
func (s *Store) Update(ctx context.Context, t *Tenant) error {
	t.Normalize()
	if err := t.Validate(); err != nil {
		return err
	}

	t.UpdatedAt = time.Now()

	defaults, supported, err := marshalScopes(t)
	if err != nil {
		return err
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE tenants SET
			name = ?, enabled = ?, base_url = ?, username = ?, password = ?,
			timeout_ms = ?, report_timeout_ms = ?, resolve_cache_ttl_sec = ?,
			tenant_header = ?, default_tenant = ?,
			mcp_token = ?, api_token = ?, dev_access_key = ?,
			default_scopes = ?, supported_scopes = ?, updated_at = ?
		 WHERE slug = ?`,
		t.Name, boolToInt(t.Enabled), t.BaseURL, t.Username, t.Password,
		t.TimeoutMs, t.ReportTimeoutMs, t.ResolveCacheTTLSec, t.TenantHeader, t.DefaultTenant,
		t.MCPToken, t.APIToken, t.DevAccessKey, defaults, supported, t.UpdatedAt.Unix(),
		t.Slug,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete удаляет только настройки базы. Выпущенные для неё OAuth-клиенты и токены остаются
// в своих таблицах — они всё равно недостижимы без маршрута и будут вычищены по TTL.
func (s *Store) Delete(ctx context.Context, slug string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tenants WHERE slug = ?`, slug)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// scanner — общий интерфейс *sql.Row и *sql.Rows, чтобы не дублировать разбор строки.
type scanner interface {
	Scan(dest ...any) error
}

func scanTenant(sc scanner) (*Tenant, error) {
	var (
		t                   Tenant
		enabled             int
		defaults, supported string
		createdAt, updated  int64
	)
	err := sc.Scan(
		&t.Slug, &t.Name, &enabled, &t.BaseURL, &t.Username, &t.Password,
		&t.TimeoutMs, &t.ReportTimeoutMs, &t.ResolveCacheTTLSec, &t.TenantHeader, &t.DefaultTenant,
		&t.MCPToken, &t.APIToken, &t.DevAccessKey, &defaults, &supported, &createdAt, &updated,
	)
	if err != nil {
		return nil, err
	}

	t.Enabled = enabled != 0
	t.CreatedAt = time.Unix(createdAt, 0)
	t.UpdatedAt = time.Unix(updated, 0)
	_ = json.Unmarshal([]byte(defaults), &t.DefaultScopes)
	_ = json.Unmarshal([]byte(supported), &t.SupportedScopes)

	return &t, nil
}

func marshalScopes(t *Tenant) (defaults, supported string, err error) {
	d, err := json.Marshal(t.DefaultScopes)
	if err != nil {
		return "", "", err
	}
	s, err := json.Marshal(t.SupportedScopes)
	if err != nil {
		return "", "", err
	}
	return string(d), string(s), nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
