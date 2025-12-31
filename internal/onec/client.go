package onec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"example.com/mcp-sales-mvp/internal/config"
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

	c.logger.Debug("1C request", "method", method, "path", path)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("1C request failed", "method", method, "path", path, "error", err)
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

	c.logger.Debug("1C response", "method", method, "path", path, "status", resp.StatusCode)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.logger.Error("1C error response", "method", method, "path", path, "status", resp.StatusCode, "body", string(respBody))
		return fmt.Errorf("1C returned status %d: %s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}

	return nil
}

func (c *Client) ResolveCustomer(ctx context.Context, query string, limit int) (*ResolveCustomerResponse, error) {
	req := ResolveRequest{
		Query: query,
		Limit: limit,
	}

	var resp ResolveCustomerResponse
	if err := c.doRequest(ctx, http.MethodPost, "/mcp/resolve/customer", req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

func (c *Client) ResolveWarehouse(ctx context.Context, query string, limit int) (*ResolveWarehouseResponse, error) {
	req := ResolveRequest{
		Query: query,
		Limit: limit,
	}

	var resp ResolveWarehouseResponse
	if err := c.doRequest(ctx, http.MethodPost, "/mcp/resolve/warehouse", req, &resp); err != nil {
		return nil, err
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
