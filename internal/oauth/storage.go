package oauth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("oauth: not found")

// Storage — обёртка над SQLite для OAuth-сущностей.
// Используется на всех путях AS (register/authorize/token) и RS (валидация токена).
type Storage struct {
	db *sql.DB
}

// NewStorage открывает БД (создавая директорию при необходимости) и запускает миграции.
func NewStorage(path string) (*Storage, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := ensureDir(dir); err != nil {
			return nil, fmt.Errorf("oauth: ensure dir: %w", err)
		}
	}

	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("oauth: open db: %w", err)
	}

	// Один writer для избежания "database is locked" на SQLite WAL под нагрузкой.
	db.SetMaxOpenConns(1)

	s := &Storage{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Storage) Close() error {
	return s.db.Close()
}

func (s *Storage) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS oauth_clients (
			client_id TEXT PRIMARY KEY,
			redirect_uris TEXT NOT NULL,
			client_name TEXT,
			token_endpoint_auth_method TEXT NOT NULL DEFAULT 'none',
			grant_types TEXT NOT NULL DEFAULT '["authorization_code","refresh_token"]',
			response_types TEXT NOT NULL DEFAULT '["code"]',
			scope TEXT,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS auth_codes (
			code TEXT PRIMARY KEY,
			client_id TEXT NOT NULL,
			redirect_uri TEXT NOT NULL,
			code_challenge TEXT NOT NULL,
			code_challenge_method TEXT NOT NULL DEFAULT 'S256',
			sub TEXT NOT NULL,
			scope TEXT NOT NULL,
			resource TEXT,
			expires_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS access_tokens (
			token TEXT PRIMARY KEY,
			client_id TEXT NOT NULL,
			sub TEXT NOT NULL,
			scope TEXT NOT NULL,
			resource TEXT,
			expires_at INTEGER NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS refresh_tokens (
			token TEXT PRIMARY KEY,
			client_id TEXT NOT NULL,
			sub TEXT NOT NULL,
			scope TEXT NOT NULL,
			resource TEXT,
			rotated_from TEXT,
			revoked INTEGER NOT NULL DEFAULT 0,
			expires_at INTEGER NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_access_tokens_expires ON access_tokens(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_codes_expires ON auth_codes(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires ON refresh_tokens(expires_at)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("oauth: migrate %q: %w", q, err)
		}
	}
	return nil
}

// --- clients ---

func (s *Storage) CreateClient(ctx context.Context, c *Client) error {
	redirects, err := json.Marshal(c.RedirectURIs)
	if err != nil {
		return err
	}
	grants, err := json.Marshal(c.GrantTypes)
	if err != nil {
		return err
	}
	responses, err := json.Marshal(c.ResponseTypes)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO oauth_clients
			(client_id, redirect_uris, client_name, token_endpoint_auth_method, grant_types, response_types, scope, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ClientID, string(redirects), c.ClientName, c.TokenEndpointAuthMethod,
		string(grants), string(responses), c.Scope, c.CreatedAt.Unix(),
	)
	return err
}

func (s *Storage) GetClient(ctx context.Context, clientID string) (*Client, error) {
	var (
		c                                                                   Client
		redirects, grants, responses                                        string
		createdAt                                                           int64
		clientName, scope, tokenEndpointAuthMethod                          sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT client_id, redirect_uris, client_name, token_endpoint_auth_method,
		        grant_types, response_types, scope, created_at
		 FROM oauth_clients WHERE client_id = ?`, clientID,
	).Scan(&c.ClientID, &redirects, &clientName, &tokenEndpointAuthMethod,
		&grants, &responses, &scope, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	c.ClientName = clientName.String
	c.TokenEndpointAuthMethod = tokenEndpointAuthMethod.String
	c.Scope = scope.String
	c.CreatedAt = time.Unix(createdAt, 0)

	_ = json.Unmarshal([]byte(redirects), &c.RedirectURIs)
	_ = json.Unmarshal([]byte(grants), &c.GrantTypes)
	_ = json.Unmarshal([]byte(responses), &c.ResponseTypes)

	return &c, nil
}

// --- auth codes ---

func (s *Storage) CreateAuthCode(ctx context.Context, a *AuthCode) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO auth_codes
			(code, client_id, redirect_uri, code_challenge, code_challenge_method,
			 sub, scope, resource, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Code, a.ClientID, a.RedirectURI, a.CodeChallenge, a.CodeChallengeMethod,
		a.Sub, a.Scope, a.Resource, a.ExpiresAt.Unix(),
	)
	return err
}

// ConsumeAuthCode — атомарно достаёт и удаляет код (одноразовое использование).
// Если код не найден или истёк — ErrNotFound.
func (s *Storage) ConsumeAuthCode(ctx context.Context, code string) (*AuthCode, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		a         AuthCode
		expiresAt int64
		resource  sql.NullString
	)
	err = tx.QueryRowContext(ctx,
		`SELECT code, client_id, redirect_uri, code_challenge, code_challenge_method,
		        sub, scope, resource, expires_at
		 FROM auth_codes WHERE code = ?`, code,
	).Scan(&a.Code, &a.ClientID, &a.RedirectURI, &a.CodeChallenge, &a.CodeChallengeMethod,
		&a.Sub, &a.Scope, &resource, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_codes WHERE code = ?`, code); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	if time.Now().Unix() > expiresAt {
		return nil, ErrNotFound
	}

	a.Resource = resource.String
	a.ExpiresAt = time.Unix(expiresAt, 0)
	return &a, nil
}

// --- access tokens ---

func (s *Storage) CreateAccessToken(ctx context.Context, t *AccessToken) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO access_tokens (token, client_id, sub, scope, resource, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.Token, t.ClientID, t.Sub, t.Scope, t.Resource, t.ExpiresAt.Unix(), t.CreatedAt.Unix(),
	)
	return err
}

// GetActiveAccessToken — lookup токена с проверкой expiry. Возвращает ErrNotFound, если истёк/не найден.
func (s *Storage) GetActiveAccessToken(ctx context.Context, token string) (*AccessToken, error) {
	var (
		t         AccessToken
		expiresAt int64
		createdAt int64
		resource  sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT token, client_id, sub, scope, resource, expires_at, created_at
		 FROM access_tokens WHERE token = ?`, token,
	).Scan(&t.Token, &t.ClientID, &t.Sub, &t.Scope, &resource, &expiresAt, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if time.Now().Unix() > expiresAt {
		return nil, ErrNotFound
	}
	t.Resource = resource.String
	t.ExpiresAt = time.Unix(expiresAt, 0)
	t.CreatedAt = time.Unix(createdAt, 0)
	return &t, nil
}

// --- refresh tokens ---

func (s *Storage) CreateRefreshToken(ctx context.Context, t *RefreshToken) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO refresh_tokens (token, client_id, sub, scope, resource, rotated_from, revoked, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Token, t.ClientID, t.Sub, t.Scope, t.Resource, t.RotatedFrom, boolToInt(t.Revoked),
		t.ExpiresAt.Unix(), t.CreatedAt.Unix(),
	)
	return err
}

// ConsumeRefreshToken — атомарно проверяет refresh, помечает revoked и возвращает данные для выпуска новой пары.
// Если токен уже revoked (replay-атака) — возвращает ErrNotFound, чтобы клиент получил 400 и переаутентифицировался.
func (s *Storage) ConsumeRefreshToken(ctx context.Context, token string) (*RefreshToken, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		t           RefreshToken
		expiresAt   int64
		createdAt   int64
		revoked     int
		resource    sql.NullString
		rotatedFrom sql.NullString
	)
	err = tx.QueryRowContext(ctx,
		`SELECT token, client_id, sub, scope, resource, rotated_from, revoked, expires_at, created_at
		 FROM refresh_tokens WHERE token = ?`, token,
	).Scan(&t.Token, &t.ClientID, &t.Sub, &t.Scope, &resource, &rotatedFrom, &revoked, &expiresAt, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if revoked != 0 || time.Now().Unix() > expiresAt {
		return nil, ErrNotFound
	}

	if _, err := tx.ExecContext(ctx, `UPDATE refresh_tokens SET revoked = 1 WHERE token = ?`, token); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	t.Resource = resource.String
	t.RotatedFrom = rotatedFrom.String
	t.Revoked = false
	t.ExpiresAt = time.Unix(expiresAt, 0)
	t.CreatedAt = time.Unix(createdAt, 0)
	return &t, nil
}

// CleanupExpired — фоновая чистка просроченных записей. Дёрнуть из горутины по таймеру.
func (s *Storage) CleanupExpired(ctx context.Context) error {
	now := time.Now().Unix()
	stmts := []string{
		`DELETE FROM auth_codes WHERE expires_at < ?`,
		`DELETE FROM access_tokens WHERE expires_at < ?`,
		`DELETE FROM refresh_tokens WHERE expires_at < ?`,
	}
	for _, q := range stmts {
		if _, err := s.db.ExecContext(ctx, q, now); err != nil {
			return err
		}
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ScopesToString — нормализация массива scope в OAuth-формат (через пробел).
func ScopesToString(scopes []string) string {
	return strings.Join(scopes, " ")
}

// ScopesFromString — обратное преобразование. Пустые элементы отбрасываются.
func ScopesFromString(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, " ")
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
