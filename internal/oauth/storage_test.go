package oauth

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"example.com/mcp-sales-mvp/internal/store"
)

// countRowsWithValue считает строки, где колонка равна ИМЕННО этому значению.
// Так проверяется, что секрет не лежит в БД в открытом виде.
func countRowsWithValue(t *testing.T, st *Storage, table, column, value string) int {
	t.Helper()
	var n int
	err := st.db.QueryRow(
		`SELECT COUNT(*) FROM `+table+` WHERE `+column+` = ?`, value).Scan(&n)
	if err != nil {
		t.Fatalf("count %s.%s: %v", table, column, err)
	}
	return n
}

// --- хранение секретов в хешированном виде ---

// TestSecretsAreHashedAtRest — доступ на чтение к файлу БД не должен давать возможности выдать
// себя за пользователя: ни код, ни токены не хранятся в том виде, в каком предъявляются.
func TestSecretsAreHashedAtRest(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)
	st := srv.storage

	verifier, challenge := pkcePair("verifier-hash-0123456789abcdefghij")
	clientID := registerClient(t, srv)
	code := codeFromRedirect(t, postAuthorize(srv, authorizeParams(clientID, challenge), testDevKey))

	// Код ещё не потрачен — строка на месте, но не под своим открытым значением.
	if n := countRowsWithValue(t, st, "auth_codes", "code", code); n != 0 {
		t.Errorf("auth code лежит в БД открытым текстом (%d строк)", n)
	}
	if n := countRowsWithValue(t, st, "auth_codes", "code", hashKey(code)); n != 1 {
		t.Errorf("auth code не найден по хешу (%d строк)", n)
	}

	tokens := decodeTokens(t, exchangeCode(srv, code, clientID, verifier))

	if n := countRowsWithValue(t, st, "access_tokens", "token", tokens.AccessToken); n != 0 {
		t.Errorf("access token лежит в БД открытым текстом (%d строк)", n)
	}
	if n := countRowsWithValue(t, st, "refresh_tokens", "token", tokens.RefreshToken); n != 0 {
		t.Errorf("refresh token лежит в БД открытым текстом (%d строк)", n)
	}
	if n := countRowsWithValue(t, st, "access_tokens", "token", hashKey(tokens.AccessToken)); n != 1 {
		t.Errorf("access token не найден по хешу (%d строк)", n)
	}

	// И при этом предъявленный клиентом токен по-прежнему работает.
	if rec := callProtected(srv, tokens.AccessToken); rec.Code != 200 {
		t.Errorf("хеширование сломало проверку токена: status = %d", rec.Code)
	}
}

// TestRotatedFromIsHashed — rotated_from хранит предыдущий токен; открытым текстом он там
// не менее опасен, чем в самой колонке token.
func TestRotatedFromIsHashed(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)
	tokens, clientID := fullFlow(t, srv)

	decodeTokens(t, postRefresh(srv, tokens.RefreshToken, clientID, ""))

	if n := countRowsWithValue(t, srv.storage, "refresh_tokens", "rotated_from", tokens.RefreshToken); n != 0 {
		t.Errorf("rotated_from хранит предыдущий токен открытым текстом (%d строк)", n)
	}
	if n := countRowsWithValue(t, srv.storage, "refresh_tokens", "rotated_from", hashKey(tokens.RefreshToken)); n != 1 {
		t.Errorf("цепочка ротации не сходится по хешу (%d строк)", n)
	}
}

// --- миграция старых БД ---

// seedLegacyPlaintext возвращает БД в состояние «до хеширования»: снимает маркер миграции и
// кладёт строки с открытыми секретами, как их писала прежняя версия.
func seedLegacyPlaintext(t *testing.T, db *sql.DB, plainToken, plainRefresh string) {
	t.Helper()

	if _, err := db.Exec(`DELETE FROM oauth_meta WHERE key = 'secrets_hashed_v1'`); err != nil {
		t.Fatalf("drop marker: %v", err)
	}
	now := time.Now()
	if _, err := db.Exec(
		`INSERT INTO access_tokens (token, tenant, client_id, sub, scope, resource, family, expires_at, created_at)
		 VALUES (?, 'tenant1', 'c1', 'legacy-user', 'mcp:resolve', ?, '', ?, ?)`,
		plainToken, testPublicURL+"/tenant1/mcp", now.Add(time.Hour).Unix(), now.Unix(),
	); err != nil {
		t.Fatalf("seed access token: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO refresh_tokens (token, tenant, client_id, sub, scope, resource, rotated_from, family, revoked, expires_at, created_at)
		 VALUES (?, 'tenant1', 'c1', 'legacy-user', 'mcp:resolve', ?, ?, '', 0, ?, ?)`,
		plainRefresh, testPublicURL+"/tenant1/mcp", plainToken, now.Add(24*time.Hour).Unix(), now.Unix(),
	); err != nil {
		t.Fatalf("seed refresh token: %v", err)
	}
}

// TestMigrationHashesExistingSecretsAndKeepsSessions — апгрейд не должен разлогинивать: строки
// хешируются на месте, и предъявленный клиентом старый токен по-прежнему находится.
func TestMigrationHashesExistingSecretsAndKeepsSessions(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "migrate.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := NewStorage(db); err != nil {
		t.Fatalf("initial storage: %v", err)
	}
	seedLegacyPlaintext(t, db, "legacy-access-token", "legacy-refresh-token")

	// Повторная инициализация = запуск новой версии на старой БД.
	st, err := NewStorage(db)
	if err != nil {
		t.Fatalf("upgrade storage: %v", err)
	}

	if n := countRowsWithValue(t, st, "access_tokens", "token", "legacy-access-token"); n != 0 {
		t.Errorf("миграция не захешировала access token (%d строк)", n)
	}
	if n := countRowsWithValue(t, st, "refresh_tokens", "rotated_from", "legacy-access-token"); n != 0 {
		t.Errorf("миграция не захешировала rotated_from (%d строк)", n)
	}

	// Главное: сессия пережила апгрейд.
	got, err := st.GetActiveAccessToken(context.Background(), "tenant1", "legacy-access-token")
	if err != nil {
		t.Fatalf("выданный до апгрейда токен перестал работать: %v", err)
	}
	if got.Sub != "legacy-user" {
		t.Errorf("sub = %q, want legacy-user", got.Sub)
	}
}

// TestMigrationIsIdempotent — второй прогон захешировал бы уже хеши и разом разлогинил всех.
func TestMigrationIsIdempotent(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "idempotent.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := NewStorage(db); err != nil {
		t.Fatalf("initial storage: %v", err)
	}
	seedLegacyPlaintext(t, db, "legacy-access-token", "legacy-refresh-token")

	for i := 0; i < 3; i++ {
		if _, err := NewStorage(db); err != nil {
			t.Fatalf("storage init %d: %v", i, err)
		}
	}

	st := &Storage{db: db}
	if n := countRowsWithValue(t, st, "access_tokens", "token", hashKey("legacy-access-token")); n != 1 {
		t.Errorf("после повторных прогонов токен не находится по своему хешу (%d строк) — двойное хеширование", n)
	}
	if _, err := st.GetActiveAccessToken(context.Background(), "tenant1", "legacy-access-token"); err != nil {
		t.Errorf("повторная миграция сломала сессию: %v", err)
	}
}

// --- отзыв семьи при replay ---

// TestReplayRevokesWholeFamily — центральный сценарий RFC 9700 §4.14.2: предъявление уже
// потраченного refresh означает, что копия в обороте. Гасить только предъявленный токен мало —
// у похитителя остаётся выданная им пара.
func TestReplayRevokesWholeFamily(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)
	stolen, clientID := fullFlow(t, srv)

	// Легитимная ротация: у клиента теперь свежая пара.
	fresh := decodeTokens(t, postRefresh(srv, stolen.RefreshToken, clientID, ""))
	if rec := callProtected(srv, fresh.AccessToken); rec.Code != 200 {
		t.Fatalf("свежий access token не работает: status = %d", rec.Code)
	}

	// Похититель предъявляет перехваченный (уже потраченный) refresh.
	replay := postRefresh(srv, stolen.RefreshToken, clientID, "")
	if replay.Code != 400 {
		t.Fatalf("replay: status = %d, want 400", replay.Code)
	}

	// Вся цепочка обязана быть погашена — и refresh, и уже выданный access.
	if rec := callProtected(srv, fresh.AccessToken); rec.Code != 401 {
		t.Errorf("access token семьи выжил после replay: status = %d, want 401", rec.Code)
	}
	if rec := postRefresh(srv, fresh.RefreshToken, clientID, ""); rec.Code != 400 {
		t.Errorf("refresh token семьи выжил после replay: status = %d, want 400", rec.Code)
	}
}

// TestReplayDoesNotAffectOtherFamilies — отзыв обязан быть точечным: параллельная сессия того же
// пользователя и клиента к инциденту отношения не имеет.
func TestReplayDoesNotAffectOtherFamilies(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)

	victim, clientID := fullFlow(t, srv)
	bystander, _ := fullFlow(t, srv) // отдельный грант — своя family

	decodeTokens(t, postRefresh(srv, victim.RefreshToken, clientID, ""))
	if rec := postRefresh(srv, victim.RefreshToken, clientID, ""); rec.Code != 400 {
		t.Fatalf("replay: status = %d, want 400", rec.Code)
	}

	if rec := callProtected(srv, bystander.AccessToken); rec.Code != 200 {
		t.Errorf("посторонняя сессия погашена вместе с чужой семьёй: status = %d, want 200", rec.Code)
	}
}

func TestRevokeFamilyIgnoresEmptyFamily(t *testing.T) {
	st := testStorage(t)
	ctx := context.Background()

	// Строка без family — наследие версии до появления колонки.
	now := time.Now()
	if err := st.CreateAccessToken(ctx, &AccessToken{
		Token: "legacy", Tenant: "tenant1", ClientID: "c", Sub: "u", Scope: "mcp:resolve",
		Resource: testPublicURL + "/tenant1/mcp", Family: "",
		ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	n, err := st.RevokeFamily(ctx, "tenant1", "")
	if err != nil {
		t.Fatalf("RevokeFamily: %v", err)
	}
	if n != 0 {
		t.Errorf("revoked = %d, want 0 — пустой family выкосил бы все легаси-строки разом", n)
	}
	if _, err := st.GetActiveAccessToken(ctx, "tenant1", "legacy"); err != nil {
		t.Errorf("легаси-токен погашен запросом по пустому family: %v", err)
	}
}

func TestRevokeFamilyIsTenantScoped(t *testing.T) {
	st := testStorage(t)
	ctx := context.Background()
	now := time.Now()

	// Одинаковый family в двух базах — коллизия невозможна на практике, но отзыв обязан
	// ограничиваться своей базой в любом случае.
	for _, tenant := range []string{"tenant1", "tenant2"} {
		if err := st.CreateAccessToken(ctx, &AccessToken{
			Token: "tok-" + tenant, Tenant: tenant, ClientID: "c", Sub: "u", Scope: "mcp:resolve",
			Resource: testPublicURL + "/" + tenant + "/mcp", Family: "shared-family",
			ExpiresAt: now.Add(time.Hour), CreatedAt: now,
		}); err != nil {
			t.Fatalf("seed %s: %v", tenant, err)
		}
	}

	if _, err := st.RevokeFamily(ctx, "tenant1", "shared-family"); err != nil {
		t.Fatalf("RevokeFamily: %v", err)
	}

	if _, err := st.GetActiveAccessToken(ctx, "tenant1", "tok-tenant1"); err == nil {
		t.Error("токен своей базы не погашен")
	}
	if _, err := st.GetActiveAccessToken(ctx, "tenant2", "tok-tenant2"); err != nil {
		t.Errorf("отзыв достал до чужой базы: %v", err)
	}
}

// TestFamilyIsInheritedAcrossRotations — цепочка из нескольких ротаций остаётся одной семьёй,
// иначе отзыв достанет только до места разрыва.
func TestFamilyIsInheritedAcrossRotations(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)
	tokens, clientID := fullFlow(t, srv)

	first := tokens
	current := tokens
	for i := 0; i < 3; i++ {
		current = decodeTokens(t, postRefresh(srv, current.RefreshToken, clientID, ""))
	}

	// Предъявляем самый первый refresh — он потрачен три ротации назад.
	if rec := postRefresh(srv, first.RefreshToken, clientID, ""); rec.Code != 400 {
		t.Fatalf("replay: status = %d, want 400", rec.Code)
	}

	// Гаснуть должен и самый свежий токен цепочки.
	if rec := callProtected(srv, current.AccessToken); rec.Code != 401 {
		t.Errorf("токен на конце цепочки выжил: status = %d, want 401 — family не наследуется", rec.Code)
	}
}

// TestRevokedAccessTokenRejected — отзыв гасит доступ немедленно, не дожидаясь expiry.
func TestRevokedAccessTokenRejected(t *testing.T) {
	st := testStorage(t)
	srv := newTestServer(t, st, "tenant1", nil)
	ctx := context.Background()
	now := time.Now()

	if err := st.CreateAccessToken(ctx, &AccessToken{
		Token: "doomed", Tenant: "tenant1", ClientID: "c", Sub: "u", Scope: "mcp:resolve",
		Resource: testPublicURL + "/tenant1/mcp", Family: "fam1",
		ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if rec := callProtected(srv, "doomed"); rec.Code != 200 {
		t.Fatalf("до отзыва: status = %d, want 200", rec.Code)
	}
	if _, err := st.RevokeFamily(ctx, "tenant1", "fam1"); err != nil {
		t.Fatalf("RevokeFamily: %v", err)
	}
	if rec := callProtected(srv, "doomed"); rec.Code != 401 {
		t.Errorf("после отзыва: status = %d, want 401", rec.Code)
	}
}

// TestReplayLeavesNoUsableSecretInDB — после инцидента в БД не должно остаться строки,
// по которой можно было бы восстановить рабочий токен.
func TestReplayLeavesNoUsableSecretInDB(t *testing.T) {
	srv := newTestServer(t, testStorage(t), "tenant1", nil)
	tokens, clientID := fullFlow(t, srv)

	decodeTokens(t, postRefresh(srv, tokens.RefreshToken, clientID, ""))
	postRefresh(srv, tokens.RefreshToken, clientID, "")

	var live int
	err := srv.storage.db.QueryRow(
		`SELECT COUNT(*) FROM refresh_tokens WHERE tenant = 'tenant1' AND revoked = 0`).Scan(&live)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if live != 0 {
		t.Errorf("после отзыва семьи осталось %d действующих refresh-токенов", live)
	}
}

func TestConsumeRefreshTokenReportsReplayDistinctly(t *testing.T) {
	st := testStorage(t)
	ctx := context.Background()
	now := time.Now()

	if err := st.CreateRefreshToken(ctx, &RefreshToken{
		Token: "r1", Tenant: "tenant1", ClientID: "c", Sub: "u", Scope: "mcp:resolve",
		Resource: testPublicURL + "/tenant1/mcp", Family: "fam1",
		ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := st.ConsumeRefreshToken(ctx, "tenant1", "r1"); err != nil {
		t.Fatalf("first consume: %v", err)
	}

	// Второй заход обязан отличаться от «не найден»: только по нему видно, что это кража.
	got, err := st.ConsumeRefreshToken(ctx, "tenant1", "r1")
	if !strings.Contains(errString(err), "replay") {
		t.Fatalf("err = %v, want ErrTokenReplay", err)
	}
	if got == nil || got.Family != "fam1" {
		t.Fatal("данные строки не возвращены — вызывающему нечем отзывать семью")
	}

	// Несуществующий токен — по-прежнему обычный «не найден».
	if _, err := st.ConsumeRefreshToken(ctx, "tenant1", "never-existed"); err == nil ||
		strings.Contains(errString(err), "replay") {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
