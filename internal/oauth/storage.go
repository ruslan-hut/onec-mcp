package oauth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrNotFound = errors.New("oauth: not found")

// ErrTokenReplay — предъявлен refresh, который уже был обменян. По RFC 9700 §4.14.2 это признак
// кражи: легитимный клиент свой токен уже потратил, значит второй предъявитель — не он.
// Отличается от ErrNotFound, чтобы вызывающий мог отозвать всю семью и записать инцидент.
var ErrTokenReplay = errors.New("oauth: refresh token replay detected")

// Токены и коды хранятся не в открытом виде, а как SHA-256 (та же hashKey, что и в кэше
// верификации): чтения файла БД тогда недостаточно, чтобы выпустить себя за пользователя.
// Секрет существует в открытом виде ровно один раз — в ответе клиенту.
// Сравнение по хешу, а не по значению, ещё и убирает необходимость constant-time сравнения:
// поиск идёт по первичному ключу, а обратить SHA-256 по времени ответа нельзя.

// Storage — OAuth-сущности поверх общего SQLite-хендла гейта (см. internal/store).
// Используется на всех путях AS (register/authorize/token) и RS (валидация токена).
// Владение соединением остаётся за вызывающим — Close здесь нет.
type Storage struct {
	db *sql.DB
}

// NewStorage прогоняет миграции OAuth-таблиц на уже открытом соединении.
func NewStorage(db *sql.DB) (*Storage, error) {
	s := &Storage{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Storage) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS oauth_clients (
			client_id TEXT PRIMARY KEY,
			tenant TEXT NOT NULL DEFAULT '',
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
			tenant TEXT NOT NULL DEFAULT '',
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
			tenant TEXT NOT NULL DEFAULT '',
			client_id TEXT NOT NULL,
			sub TEXT NOT NULL,
			scope TEXT NOT NULL,
			resource TEXT,
			expires_at INTEGER NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS refresh_tokens (
			token TEXT PRIMARY KEY,
			tenant TEXT NOT NULL DEFAULT '',
			client_id TEXT NOT NULL,
			sub TEXT NOT NULL,
			scope TEXT NOT NULL,
			resource TEXT,
			rotated_from TEXT,
			revoked INTEGER NOT NULL DEFAULT 0,
			expires_at INTEGER NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS oauth_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
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

	// Досыпаем tenant в БД, созданные до мультитенантности. Старые строки получают tenant=''
	// и не совпадут ни с одним слагом — то есть выпущенные до апгрейда токены становятся
	// невалидными, а клиентов Claude/ChatGPT надо переподключить. Это осознанное решение.
	for _, table := range []string{"oauth_clients", "auth_codes", "access_tokens", "refresh_tokens"} {
		if err := s.addColumnIfMissing(table, "tenant", "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}

	// family — общий идентификатор всей цепочки, выросшей из одного грант-события. Нужен, чтобы
	// при обнаружении replay гасить не только предъявленный токен, но и всё, что из него выросло.
	for _, table := range []string{"access_tokens", "refresh_tokens"} {
		if err := s.addColumnIfMissing(table, "family", "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}

	// У refresh_tokens revoked есть с самого начала; access_tokens его не имели — гасить их
	// досрочно было незачем, пока не появился отзыв семьи.
	if err := s.addColumnIfMissing("access_tokens", "revoked", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}

	return s.migrateHashSecrets()
}

// migrateHashSecrets переводит уже лежащие в БД коды и токены на хранение по SHA-256.
// Хешируем существующие значения на месте, а не выбрасываем их: активные сессии переживают
// апгрейд — предъявленный клиентом токен даст тот же хеш и найдётся.
//
// Прогон одноразовый и отмечается в oauth_meta: повторный захешировал бы уже хеши и разом
// разлогинил всех.
func (s *Storage) migrateHashSecrets() error {
	const marker = "secrets_hashed_v1"

	var done string
	err := s.db.QueryRow(`SELECT value FROM oauth_meta WHERE key = ?`, marker).Scan(&done)
	if err == nil {
		return nil // уже прогнано
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("oauth: read migration marker: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// (таблица, колонка-секрет) — rotated_from хранит значение предыдущего токена и обязан
	// переехать вместе с ним, иначе цепочка ротации перестанет сходиться.
	targets := []struct{ table, column string }{
		{"auth_codes", "code"},
		{"access_tokens", "token"},
		{"refresh_tokens", "token"},
		{"refresh_tokens", "rotated_from"},
	}

	for _, tgt := range targets {
		rows, err := tx.Query(fmt.Sprintf(
			`SELECT rowid, %s FROM %s WHERE %s IS NOT NULL AND %s != ''`,
			tgt.column, tgt.table, tgt.column, tgt.column))
		if err != nil {
			return fmt.Errorf("oauth: scan %s.%s: %w", tgt.table, tgt.column, err)
		}

		type row struct {
			id  int64
			val string
		}
		var batch []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.val); err != nil {
				_ = rows.Close()
				return fmt.Errorf("oauth: scan %s.%s: %w", tgt.table, tgt.column, err)
			}
			batch = append(batch, r)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("oauth: scan %s.%s: %w", tgt.table, tgt.column, err)
		}
		_ = rows.Close()

		for _, r := range batch {
			if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s SET %s = ? WHERE rowid = ?`, tgt.table, tgt.column),
				hashKey(r.val), r.id); err != nil {
				return fmt.Errorf("oauth: hash %s.%s: %w", tgt.table, tgt.column, err)
			}
		}
	}

	if _, err := tx.Exec(`INSERT INTO oauth_meta (key, value) VALUES (?, ?)`, marker, "1"); err != nil {
		return fmt.Errorf("oauth: write migration marker: %w", err)
	}
	return tx.Commit()
}

// addColumnIfMissing — идемпотентный ALTER TABLE: SQLite не умеет ADD COLUMN IF NOT EXISTS,
// поэтому сначала смотрим в PRAGMA table_info.
func (s *Storage) addColumnIfMissing(table, column, decl string) error {
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return fmt.Errorf("oauth: inspect %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("oauth: inspect %s: %w", table, err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("oauth: inspect %s: %w", table, err)
	}

	if _, err := s.db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, decl)); err != nil {
		return fmt.Errorf("oauth: add column %s.%s: %w", table, column, err)
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
			(client_id, tenant, redirect_uris, client_name, token_endpoint_auth_method, grant_types, response_types, scope, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ClientID, c.Tenant, string(redirects), c.ClientName, c.TokenEndpointAuthMethod,
		string(grants), string(responses), c.Scope, c.CreatedAt.Unix(),
	)
	return err
}

// GetClient — клиент виден только внутри своей базы: client_id, зарегистрированный
// в другом тенанте, вернёт ErrNotFound.
func (s *Storage) GetClient(ctx context.Context, tenant, clientID string) (*Client, error) {
	var (
		c                                          Client
		redirects, grants, responses               string
		createdAt                                  int64
		clientName, scope, tokenEndpointAuthMethod sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT client_id, tenant, redirect_uris, client_name, token_endpoint_auth_method,
		        grant_types, response_types, scope, created_at
		 FROM oauth_clients WHERE client_id = ? AND tenant = ?`, clientID, tenant,
	).Scan(&c.ClientID, &c.Tenant, &redirects, &clientName, &tokenEndpointAuthMethod,
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
			(code, tenant, client_id, redirect_uri, code_challenge, code_challenge_method,
			 sub, scope, resource, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		hashKey(a.Code), a.Tenant, a.ClientID, a.RedirectURI, a.CodeChallenge, a.CodeChallengeMethod,
		a.Sub, a.Scope, a.Resource, a.ExpiresAt.Unix(),
	)
	return err
}

// ConsumeAuthCode — атомарно достаёт и удаляет код (одноразовое использование).
// Если код не найден, принадлежит другой базе или истёк — ErrNotFound.
func (s *Storage) ConsumeAuthCode(ctx context.Context, tenant, code string) (*AuthCode, error) {
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
	hashed := hashKey(code)
	err = tx.QueryRowContext(ctx,
		`SELECT code, tenant, client_id, redirect_uri, code_challenge, code_challenge_method,
		        sub, scope, resource, expires_at
		 FROM auth_codes WHERE code = ? AND tenant = ?`, hashed, tenant,
	).Scan(&a.Code, &a.Tenant, &a.ClientID, &a.RedirectURI, &a.CodeChallenge, &a.CodeChallengeMethod,
		&a.Sub, &a.Scope, &resource, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	// В БД лежит хеш; наружу отдаём предъявленный код, чтобы вызывающий работал с тем же
	// значением, что и клиент.
	a.Code = code

	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_codes WHERE code = ?`, hashed); err != nil {
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
		`INSERT INTO access_tokens (token, tenant, client_id, sub, scope, resource, family, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		hashKey(t.Token), t.Tenant, t.ClientID, t.Sub, t.Scope, t.Resource, t.Family,
		t.ExpiresAt.Unix(), t.CreatedAt.Unix(),
	)
	return err
}

// GetActiveAccessToken — lookup токена в пределах базы, с проверкой expiry.
// ErrNotFound, если токен истёк, не найден или выпущен для другой базы.
func (s *Storage) GetActiveAccessToken(ctx context.Context, tenant, token string) (*AccessToken, error) {
	var (
		t         AccessToken
		expiresAt int64
		createdAt int64
		resource  sql.NullString
		family    sql.NullString
		revoked   int
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT token, tenant, client_id, sub, scope, resource, family, revoked, expires_at, created_at
		 FROM access_tokens WHERE token = ? AND tenant = ?`, hashKey(token), tenant,
	).Scan(&t.Token, &t.Tenant, &t.ClientID, &t.Sub, &t.Scope, &resource, &family, &revoked,
		&expiresAt, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	// revoked ставится при обнаружении replay: доступ гасится сразу, не дожидаясь expiry.
	if revoked != 0 || time.Now().Unix() > expiresAt {
		return nil, ErrNotFound
	}
	t.Token = token
	t.Family = family.String
	t.Resource = resource.String
	t.ExpiresAt = time.Unix(expiresAt, 0)
	t.CreatedAt = time.Unix(createdAt, 0)
	return &t, nil
}

// --- refresh tokens ---

func (s *Storage) CreateRefreshToken(ctx context.Context, t *RefreshToken) error {
	// rotated_from хранит хеш предыдущего токена — в колонке не должно оставаться значений,
	// которыми можно воспользоваться.
	rotatedFrom := ""
	if t.RotatedFrom != "" {
		rotatedFrom = hashKey(t.RotatedFrom)
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO refresh_tokens (token, tenant, client_id, sub, scope, resource, rotated_from, family, revoked, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		hashKey(t.Token), t.Tenant, t.ClientID, t.Sub, t.Scope, t.Resource, rotatedFrom, t.Family,
		boolToInt(t.Revoked), t.ExpiresAt.Unix(), t.CreatedAt.Unix(),
	)
	return err
}

// ConsumeRefreshToken — атомарно проверяет refresh в пределах базы, помечает revoked и возвращает
// данные для выпуска новой пары. Истёкший или чужой токен — ErrNotFound.
//
// Уже израсходованный токен — отдельный случай: возвращается ErrTokenReplay вместе с данными строки,
// чтобы вызывающий мог погасить всю семью (RFC 9700 §4.14.2). Легитимный клиент свой токен уже
// обменял, так что второй предъявитель — тот, кто его добыл; кто из них двоих пришёл вторым,
// неизвестно, поэтому единственный безопасный ход — отозвать цепочку целиком.
func (s *Storage) ConsumeRefreshToken(ctx context.Context, tenant, token string) (*RefreshToken, error) {
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
		family      sql.NullString
	)
	hashed := hashKey(token)
	err = tx.QueryRowContext(ctx,
		`SELECT token, tenant, client_id, sub, scope, resource, rotated_from, family, revoked, expires_at, created_at
		 FROM refresh_tokens WHERE token = ? AND tenant = ?`, hashed, tenant,
	).Scan(&t.Token, &t.Tenant, &t.ClientID, &t.Sub, &t.Scope, &resource, &rotatedFrom, &family,
		&revoked, &expiresAt, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	t.Token = token
	t.Resource = resource.String
	t.RotatedFrom = rotatedFrom.String
	t.Family = family.String
	t.ExpiresAt = time.Unix(expiresAt, 0)
	t.CreatedAt = time.Unix(createdAt, 0)

	// Повторное предъявление уже потраченного токена — сигнал кражи, а не просто отказ.
	// Данные строки возвращаем вместе с ошибкой: по ним вызывающий гасит семью.
	if revoked != 0 {
		t.Revoked = true
		return &t, ErrTokenReplay
	}
	if time.Now().Unix() > expiresAt {
		return nil, ErrNotFound
	}

	if _, err := tx.ExecContext(ctx, `UPDATE refresh_tokens SET revoked = 1 WHERE token = ?`, hashed); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	t.Revoked = false
	return &t, nil
}

// RevokeFamily гасит все токены одной цепочки — и refresh, и access, чтобы у похитителя не
// осталось действующего доступа до истечения TTL.
//
// Пустой family игнорируется: такие строки остались от версии до появления колонки, и запрос
// по '' выкосил бы их все разом.
func (s *Storage) RevokeFamily(ctx context.Context, tenant, family string) (int64, error) {
	if family == "" {
		return 0, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var total int64
	for _, q := range []string{
		`UPDATE refresh_tokens SET revoked = 1 WHERE family = ? AND tenant = ? AND revoked = 0`,
		`UPDATE access_tokens SET revoked = 1 WHERE family = ? AND tenant = ? AND revoked = 0`,
	} {
		res, err := tx.ExecContext(ctx, q, family, tenant)
		if err != nil {
			return 0, err
		}
		if n, err := res.RowsAffected(); err == nil {
			total += n
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return total, nil
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
