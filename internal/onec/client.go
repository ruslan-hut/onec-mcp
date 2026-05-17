package onec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"example.com/mcp-sales-mvp/internal/config"
	"example.com/mcp-sales-mvp/internal/oauth"
)

type Client struct {
	httpClient    *http.Client
	baseURL       string
	authType      string
	username      string
	password      string
	tenantHeader  string
	defaultTenant string
	logger        *slog.Logger
	resolveCache  *resolveCache
}

func NewClient(cfg *config.OneCConfig, logger *slog.Logger) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: cfg.Timeout(),
		},
		baseURL:       cfg.BaseURL,
		authType:      cfg.Auth.Type,
		username:      cfg.Auth.Username,
		password:      cfg.Auth.Password,
		tenantHeader:  cfg.TenantHeader,
		defaultTenant: cfg.DefaultTenant,
		logger:        logger,
		resolveCache:  newResolveCache(cfg.ResolveCacheTTL()),
	}
}

func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if c.tenantHeader != "" && c.defaultTenant != "" {
		req.Header.Set(c.tenantHeader, c.defaultTenant)
	}

	switch c.authType {
	case "basic":
		req.SetBasicAuth(c.username, c.password)
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+c.password)
	}

	// Прокидываем sub/scopes резолвнутого пользователя в 1С (defense in depth).
	// 1С использует X-MCP-Scopes для per-endpoint ACL — даже при компрометации гейта
	// ключ с mcp:resolve не сможет вызвать report endpoints.
	if auth := oauth.FromContext(ctx); auth != nil {
		req.Header.Set("X-MCP-Sub", auth.Sub)
		// Comma-separated, чтобы парсинг в 1С был прямолинейным (см. RequireScope в HTTPServices/MCP).
		req.Header.Set("X-MCP-Scopes", strings.Join(auth.Scopes, ","))
	}

	start := time.Now()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("1C request", "method", method, "path", path, "error", err, "duration_ms", time.Since(start).Milliseconds())
		return fmt.Errorf("request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			c.logger.Error("1C response body close failed", "method", method, "path", path, "error", err)
		}
	}(resp.Body)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	duration := time.Since(start).Milliseconds()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.logger.Error("1C request", "method", method, "path", path, "status", resp.StatusCode, "duration_ms", duration, "body", string(respBody))

		// Пробуем достать структурированный {error, message} из тела —
		// 1С отдаёт его осмысленно, и клиент сможет показать пользователю реальную причину
		apiErr := &APIError{StatusCode: resp.StatusCode}
		if json.Unmarshal(respBody, apiErr) == nil && (apiErr.Code != "" || apiErr.Message != "") {
			return apiErr
		}

		return fmt.Errorf("1C returned status %d: %s", resp.StatusCode, string(respBody))
	}

	c.logger.Debug("1C request", "method", method, "path", path, "status", resp.StatusCode, "duration_ms", duration)

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}

	return nil
}

func (c *Client) ResolveCustomer(ctx context.Context, query string, limit int) (*ResolveCustomerResponse, error) {
	if cached, ok := c.resolveCache.Get("customer", query, limit); ok {
		var resp ResolveCustomerResponse
		if err := json.Unmarshal(cached, &resp); err == nil {
			return &resp, nil
		}
	}

	req := ResolveRequest{Query: query, Limit: limit}
	var resp ResolveCustomerResponse
	if err := c.doRequest(ctx, http.MethodPost, "/mcp/resolve/customer", req, &resp); err != nil {
		return nil, err
	}

	if payload, err := json.Marshal(&resp); err == nil {
		c.resolveCache.Set("customer", query, limit, payload)
	}
	return &resp, nil
}

func (c *Client) ResolveWarehouse(ctx context.Context, query string, limit int) (*ResolveWarehouseResponse, error) {
	if cached, ok := c.resolveCache.Get("warehouse", query, limit); ok {
		var resp ResolveWarehouseResponse
		if err := json.Unmarshal(cached, &resp); err == nil {
			return &resp, nil
		}
	}

	req := ResolveRequest{Query: query, Limit: limit}
	var resp ResolveWarehouseResponse
	if err := c.doRequest(ctx, http.MethodPost, "/mcp/resolve/warehouse", req, &resp); err != nil {
		return nil, err
	}

	if payload, err := json.Marshal(&resp); err == nil {
		c.resolveCache.Set("warehouse", query, limit, payload)
	}
	return &resp, nil
}

func (c *Client) ResolveProduct(ctx context.Context, query string, limit int) (*ResolveProductResponse, error) {
	if cached, ok := c.resolveCache.Get("product", query, limit); ok {
		var resp ResolveProductResponse
		if err := json.Unmarshal(cached, &resp); err == nil {
			return &resp, nil
		}
	}

	req := ResolveRequest{Query: query, Limit: limit}
	var resp ResolveProductResponse
	if err := c.doRequest(ctx, http.MethodPost, "/mcp/resolve/product", req, &resp); err != nil {
		return nil, err
	}

	if payload, err := json.Marshal(&resp); err == nil {
		c.resolveCache.Set("product", query, limit, payload)
	}
	return &resp, nil
}

func (c *Client) SalesReport(ctx context.Context, req *SalesReportRequest) (*SalesReportResponse, error) {
	var resp SalesReportResponse
	if err := c.doRequest(ctx, http.MethodPost, "/mcp/reports/sales", req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

func (c *Client) StockReport(ctx context.Context, req *StockReportRequest) (*StockReportResponse, error) {
	var resp StockReportResponse
	if err := c.doRequest(ctx, http.MethodPost, "/mcp/reports/stock", req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// TopProducts / CustomerSummary возвращаются как json.RawMessage —
// гейту достаточно прокинуть тело наверх, без декомпозиции в типизированную структуру.
// Это позволяет добавлять поля в 1С-стороне без правки гейта.
func (c *Client) TopProducts(ctx context.Context, req *TopProductsRequest) (json.RawMessage, error) {
	var resp json.RawMessage
	if err := c.doRequest(ctx, http.MethodPost, "/mcp/reports/top_products", req, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) CustomerSummary(ctx context.Context, req *CustomerSummaryRequest) (json.RawMessage, error) {
	var resp json.RawMessage
	if err := c.doRequest(ctx, http.MethodPost, "/mcp/reports/customer_summary", req, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// VerifyMCPKey проверяет MCP-ключ через 1С. Возвращает APIError со статусом 401, если ключ невалиден —
// вызывающий код должен это отличать от network/server ошибок.
func (c *Client) VerifyMCPKey(ctx context.Context, key string) (*AuthVerifyResponse, error) {
	req := AuthVerifyRequest{Key: key}
	var resp AuthVerifyResponse
	if err := c.doRequest(ctx, http.MethodPost, "/mcp/auth/verify", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
