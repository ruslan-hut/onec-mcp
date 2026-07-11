// Package admin — веб-интерфейс управления базами 1С по пути /admin.
// Закрыт Basic-аутентификацией с логином/паролем из конфига: администраторы не хранятся в БД,
// в БД лежат только сами базы.
package admin

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"example.com/mcp-sales-mvp/internal/tenant"
)

// Reloader — то, что нужно дёрнуть после любой правки, чтобы изменения подхватились без
// перезапуска процесса. В main.go это api.Registry.
type Reloader interface {
	Reload(ctx context.Context) error
}

// Config — параметры интерфейса. PublicURL нужен только для показа готовых URL коннекторов
// на странице списка; пустой — колонка просто не заполняется.
type Config struct {
	Username  string
	Password  string
	PublicURL string
	// OAuthEnabled влияет на подсказки в форме: при OAuth статический mcp_token не используется.
	OAuthEnabled bool
	// GlobalScopes — что подставится, если у базы не задан свой список (показываем как плейсхолдер).
	GlobalScopes []string
}

type Handler struct {
	cfg      Config
	store    *tenant.Store
	reloader Reloader
	logger   *slog.Logger
}

func New(cfg Config, store *tenant.Store, reloader Reloader, logger *slog.Logger) *Handler {
	return &Handler{cfg: cfg, store: store, reloader: reloader, logger: logger}
}

// Routes — суброутер, монтируемый на /admin.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(noFraming)
	r.Use(h.basicAuth)
	r.Use(h.sameOrigin)

	r.Get("/", h.list)
	r.Get("/new", h.newForm)
	r.Post("/new", h.create)
	r.Get("/{slug}", h.editForm)
	r.Post("/{slug}", h.update)
	r.Post("/{slug}/delete", h.delete)

	return r
}

// noFraming запрещает встраивать /admin в iframe. Без этого Basic-аутентификация играет против
// нас: браузер сам подставит закэшированную учётку в фрейм с настоящей страницей, и админа можно
// кликджекнуть в отправку реальной формы. Проверка Origin такое не ловит — Origin там наш же.
func noFraming(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

// basicAuth — единственный барьер перед /admin. Сравнение обеих половин constant-time,
// чтобы по времени ответа нельзя было подобрать логин или пароль посимвольно.
func (h *Handler) basicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		userOK := subtle.ConstantTimeCompare([]byte(user), []byte(h.cfg.Username)) == 1
		passOK := subtle.ConstantTimeCompare([]byte(pass), []byte(h.cfg.Password)) == 1
		if !ok || !userOK || !passOK {
			h.logger.Warn("admin.auth.failed", "remote", r.RemoteAddr, "path", r.URL.Path)
			w.Header().Set("WWW-Authenticate", `Basic realm="onec-mcp admin", charset="UTF-8"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin — защита от CSRF. С Basic-аутентификацией браузер сам подставляет учётку в любой
// запрос, включая инициированный чужой страницей, поэтому мутирующие методы принимаются только
// если Origin совпадает с хостом запроса. Браузеры шлют Origin на всех POST; запросы без Origin
// (curl, скрипты) не несут browser-managed credentials и потому не подвержены CSRF.
func (h *Handler) sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || !strings.EqualFold(u.Host, r.Host) {
				h.logger.Warn("admin.csrf.blocked", "origin", origin, "host", r.Host)
				http.Error(w, "cross-origin request rejected", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	tenants, err := h.store.List(r.Context())
	if err != nil {
		h.serverError(w, "не удалось прочитать список баз", err)
		return
	}

	rows := make([]listRow, 0, len(tenants))
	for _, t := range tenants {
		rows = append(rows, listRow{
			Tenant: t,
			MCPURL: h.mcpURL(t.Slug),
		})
	}

	h.render(w, listTemplate, listData{
		Tenants: rows,
		Notice:  r.URL.Query().Get("notice"),
	})
}

func (h *Handler) newForm(w http.ResponseWriter, r *http.Request) {
	h.render(w, formTemplate, h.formData(&tenant.Tenant{
		Enabled:            true,
		TimeoutMs:          tenant.DefaultTimeoutMs,
		ReportTimeoutMs:    tenant.DefaultReportTimeoutMs,
		ResolveCacheTTLSec: tenant.DefaultResolveCacheTTLSec,
	}, true, ""))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	t, err := parseForm(r)
	if err != nil {
		h.render(w, formTemplate, h.formData(t, true, err.Error()))
		return
	}

	if err := h.store.Create(r.Context(), t); err != nil {
		if errors.Is(err, tenant.ErrExists) {
			h.render(w, formTemplate, h.formData(t, true, "база с таким слагом уже существует"))
			return
		}
		// Ошибки валидации возвращаются из Create как есть — показываем их в форме,
		// а не как 500: это ввод пользователя, а не сбой.
		h.render(w, formTemplate, h.formData(t, true, err.Error()))
		return
	}

	h.reload()
	h.logger.Info("admin.tenant.created", "slug", t.Slug)
	http.Redirect(w, r, "/admin/?notice="+url.QueryEscape("База "+t.Slug+" создана"), http.StatusSeeOther)
}

func (h *Handler) editForm(w http.ResponseWriter, r *http.Request) {
	t, err := h.store.Get(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		h.notFound(w, err)
		return
	}
	h.render(w, formTemplate, h.formData(t, false, ""))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	existing, err := h.store.Get(r.Context(), slug)
	if err != nil {
		h.notFound(w, err)
		return
	}

	t, err := parseForm(r)

	// Слаг берём из пути, а не из формы: поле в форме disabled, и браузер его не отправляет.
	// Заодно это защита от переименования — слаг зашит в URL коннекторов, в issuer и в привязку
	// выпущенных токенов, менять его нельзя. Присваиваем до проверки err, иначе форма
	// перерисуется без слага и с битым action.
	t.Slug = slug

	if err != nil {
		h.render(w, formTemplate, h.formData(t, false, err.Error()))
		return
	}

	// Пустое поле пароля означает «оставить как есть» — иначе редактирование любого другого поля
	// затирало бы пароль 1С, который в форме не показывается.
	if t.Password == "" {
		t.Password = existing.Password
	}

	if err := h.store.Update(r.Context(), t); err != nil {
		if errors.Is(err, tenant.ErrNotFound) {
			h.notFound(w, err)
			return
		}
		h.render(w, formTemplate, h.formData(t, false, err.Error()))
		return
	}

	h.reload()
	h.logger.Info("admin.tenant.updated", "slug", slug)
	http.Redirect(w, r, "/admin/?notice="+url.QueryEscape("База "+slug+" сохранена"), http.StatusSeeOther)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	if err := h.store.Delete(r.Context(), slug); err != nil {
		if errors.Is(err, tenant.ErrNotFound) {
			h.notFound(w, err)
			return
		}
		h.serverError(w, "не удалось удалить базу", err)
		return
	}

	h.reload()
	h.logger.Info("admin.tenant.deleted", "slug", slug)
	http.Redirect(w, r, "/admin/?notice="+url.QueryEscape("База "+slug+" удалена"), http.StatusSeeOther)
}

// reload применяет изменения к живому роутеру. Ошибка здесь не откатывает запись в БД:
// данные уже сохранены, и следующий успешный reload (или рестарт) их подхватит — поэтому
// логируем и идём дальше, а не показываем пользователю 500 поверх успешного сохранения.
//
// Контекст свой, а не запросa: reload пересобирает обвязки ВСЕХ баз, и отвалившийся браузер
// администратора не должен оборвать это на середине, оставив реестр на устаревших данных.
func (h *Handler) reload() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := h.reloader.Reload(ctx); err != nil {
		h.logger.Error("admin.reload.failed", "error", err)
	}
}

// parseForm собирает Tenant из формы. Ошибка возвращается вместе с частично заполненным
// объектом — чтобы перерисовать форму с введёнными значениями, а не с пустыми полями.
func parseForm(r *http.Request) (*tenant.Tenant, error) {
	if err := r.ParseForm(); err != nil {
		return &tenant.Tenant{}, errors.New("не удалось разобрать форму")
	}

	t := &tenant.Tenant{
		Slug:          strings.TrimSpace(r.PostForm.Get("slug")),
		Name:          strings.TrimSpace(r.PostForm.Get("name")),
		Enabled:       r.PostForm.Get("enabled") != "",
		BaseURL:       strings.TrimSpace(r.PostForm.Get("base_url")),
		Username:      strings.TrimSpace(r.PostForm.Get("username")),
		Password:      r.PostForm.Get("password"),
		TenantHeader:  strings.TrimSpace(r.PostForm.Get("tenant_header")),
		DefaultTenant: strings.TrimSpace(r.PostForm.Get("default_tenant")),
		MCPToken:      strings.TrimSpace(r.PostForm.Get("mcp_token")),
		APIToken:      strings.TrimSpace(r.PostForm.Get("api_token")),
		DevAccessKey:  strings.TrimSpace(r.PostForm.Get("dev_access_key")),

		DefaultScopes:   splitScopes(r.PostForm.Get("default_scopes")),
		SupportedScopes: splitScopes(r.PostForm.Get("supported_scopes")),
	}

	var err error
	if t.TimeoutMs, err = atoiField(r.PostForm.Get("timeout_ms"), "таймаут"); err != nil {
		return t, err
	}
	if t.ReportTimeoutMs, err = atoiField(r.PostForm.Get("report_timeout_ms"), "таймаут отчётов"); err != nil {
		return t, err
	}
	if t.ResolveCacheTTLSec, err = atoiField(r.PostForm.Get("resolve_cache_ttl_sec"), "TTL кэша"); err != nil {
		return t, err
	}

	return t, nil
}

func atoiField(raw, name string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New(name + ": ожидается целое число")
	}
	return v, nil
}

// splitScopes принимает scope через пробел, запятую или перенос строки — как удобнее в textarea.
func splitScopes(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\n' || r == '\r' || r == '\t'
	})
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// mcpURL — готовый адрес коннектора для базы. Пустой public_url = показывать нечего.
func (h *Handler) mcpURL(slug string) string {
	if h.cfg.PublicURL == "" {
		return ""
	}
	return strings.TrimRight(h.cfg.PublicURL, "/") + "/" + slug + "/mcp"
}
