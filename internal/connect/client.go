package connect

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/example/connectorctl/internal/config"
	"github.com/example/connectorctl/internal/model"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	auth       config.RESTAuth
	username   string
	password   string
	token      string
	limiter    *tokenBucket
	retries    int
}

func NewClient(cfg config.RESTConfig) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	parsedBase, err := url.Parse(base)
	if err != nil || parsedBase.Scheme == "" || parsedBase.Host == "" {
		return nil, fmt.Errorf("invalid Connect REST URL %q", base)
	}
	tlsConfig, err := buildTLSConfig(cfg.TLS)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: tlsConfig, MaxIdleConns: 100, MaxIdleConnsPerHost: 50, IdleConnTimeout: 90 * time.Second}
	timeout := 30 * time.Second
	if cfg.Timeout != "" {
		parsed, err := time.ParseDuration(cfg.Timeout)
		if err != nil {
			return nil, err
		}
		timeout = parsed
	}
	rps := cfg.RPS
	if rps <= 0 {
		rps = 10
	}
	burst := cfg.Burst
	if burst <= 0 {
		burst = 10
	}
	retries := cfg.Retries
	if retries <= 0 {
		retries = 4
	}
	c := &Client{baseURL: base, httpClient: &http.Client{Transport: transport, Timeout: timeout}, auth: cfg.Auth, limiter: newTokenBucket(rps, burst), retries: retries}
	switch strings.ToLower(cfg.Auth.Type) {
	case "", "none":
	case "basic":
		c.username = os.Getenv(cfg.Auth.UsernameEnv)
		c.password = os.Getenv(cfg.Auth.PasswordEnv)
		if c.username == "" || c.password == "" {
			return nil, fmt.Errorf("basic authentication environment variables %s and %s must be set", cfg.Auth.UsernameEnv, cfg.Auth.PasswordEnv)
		}
	case "bearer":
		c.token = os.Getenv(cfg.Auth.TokenEnv)
		if c.token == "" {
			return nil, fmt.Errorf("bearer authentication environment variable %s must be set", cfg.Auth.TokenEnv)
		}
	default:
		return nil, fmt.Errorf("unsupported REST auth type %q", cfg.Auth.Type)
	}
	return c, nil
}

func (c *Client) ListConnectors(ctx context.Context) ([]string, error) {
	var names []string
	if err := c.doJSON(ctx, http.MethodGet, "/connectors", nil, &names); err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

func (c *Client) GetConfig(ctx context.Context, name string) (map[string]string, error) {
	var raw map[string]any
	if err := c.doJSON(ctx, http.MethodGet, "/connectors/"+url.PathEscape(name)+"/config", nil, &raw); err != nil {
		return nil, err
	}
	return normalizeConfig(raw)
}

func (c *Client) GetStatus(ctx context.Context, name string) (model.ConnectorStatus, error) {
	var status model.ConnectorStatus
	err := c.doJSON(ctx, http.MethodGet, "/connectors/"+url.PathEscape(name)+"/status", nil, &status)
	return status, err
}

func (c *Client) ListPlugins(ctx context.Context) ([]model.PluginInfo, error) {
	var plugins []model.PluginInfo
	if err := c.doJSON(ctx, http.MethodGet, "/connector-plugins?connectorsOnly=true", nil, &plugins); err != nil {
		return nil, err
	}
	return plugins, nil
}

func (c *Client) ValidateConfig(ctx context.Context, className string, cfg map[string]string) (model.ConfigValidationResponse, error) {
	var response model.ConfigValidationResponse
	path := "/connector-plugins/" + url.PathEscape(className) + "/config/validate"
	err := c.doJSON(ctx, http.MethodPut, path, cfg, &response)
	return response, err
}

func (c *Client) doJSON(ctx context.Context, method, path string, requestBody any, responseTarget any) error {
	var bodyBytes []byte
	var err error
	if requestBody != nil {
		bodyBytes, err = json.Marshal(requestBody)
		if err != nil {
			return err
		}
	}
	for attempt := 0; attempt <= c.retries; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return err
		}
		var body io.Reader
		if bodyBytes != nil {
			body = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		if bodyBytes != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		c.applyAuth(req)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			if attempt < c.retries && retryableNetworkError(err) {
				if err := sleepBackoff(ctx, attempt); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("Connect REST %s %s failed: %w", method, path, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if attempt < c.retries && retryableStatus(resp.StatusCode) {
				if err := sleepRetry(ctx, attempt, resp.Header.Get("Retry-After")); err != nil {
					return err
				}
				continue
			}
			return decodeAPIError(method, path, resp.StatusCode, data)
		}
		if responseTarget == nil || len(bytes.TrimSpace(data)) == 0 {
			return nil
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if err := dec.Decode(responseTarget); err != nil {
			return fmt.Errorf("decode Connect REST response for %s %s: %w", method, path, err)
		}
		return nil
	}
	return fmt.Errorf("Connect REST %s %s exhausted retries", method, path)
}

func (c *Client) applyAuth(req *http.Request) {
	switch strings.ToLower(c.auth.Type) {
	case "basic":
		req.SetBasicAuth(c.username, c.password)
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

func buildTLSConfig(cfg config.RESTTLS) (*tls.Config, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.InsecureSkipVerify} // #nosec G402 -- controlled by explicit platform configuration.
	if cfg.CAFile != "" {
		data, err := os.ReadFile(filepath.Clean(cfg.CAFile))
		if err != nil {
			return nil, fmt.Errorf("read CA file: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if ok := pool.AppendCertsFromPEM(data); !ok {
			return nil, errors.New("CA file contains no valid certificates")
		}
		tlsConfig.RootCAs = pool
	}
	if cfg.CertFile != "" || cfg.KeyFile != "" {
		if cfg.CertFile == "" || cfg.KeyFile == "" {
			return nil, errors.New("both tls.certFile and tls.keyFile are required for mTLS")
		}
		cert, err := tls.LoadX509KeyPair(filepath.Clean(cfg.CertFile), filepath.Clean(cfg.KeyFile))
		if err != nil {
			return nil, fmt.Errorf("load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	return tlsConfig, nil
}

func retryableStatus(status int) bool {
	return status == http.StatusConflict || status == http.StatusTooManyRequests || status == 500 || status == 502 || status == 503 || status == 504
}
func retryableNetworkError(err error) bool {
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}
func sleepBackoff(ctx context.Context, attempt int) error {
	return sleepDuration(ctx, 150*time.Millisecond*time.Duration(1<<min(attempt, 4)))
}

func sleepRetry(ctx context.Context, attempt int, retryAfter string) error {
	d := 150 * time.Millisecond * time.Duration(1<<min(attempt, 4))
	if parsed, ok := parseRetryAfter(retryAfter, time.Now()); ok && parsed > d {
		d = parsed
	}
	const maxRetryDelay = 30 * time.Second
	if d > maxRetryDelay {
		d = maxRetryDelay
	}
	return sleepDuration(ctx, d)
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second, true
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	if !when.After(now) {
		return 0, true
	}
	return when.Sub(now), true
}

func sleepDuration(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func decodeAPIError(method, path string, status int, data []byte) error {
	// Kafka Connect error messages may echo connector configuration, including
	// credential values downloaded during ADOPT. Preserve the machine-readable
	// error code but deliberately suppress the response message from standard
	// output and JSON reports.
	var apiErr struct {
		ErrorCode int `json:"error_code"`
	}
	if json.Unmarshal(data, &apiErr) == nil && apiErr.ErrorCode != 0 {
		return fmt.Errorf("Connect REST %s %s returned HTTP %d (error_code=%d; response message suppressed)", method, path, status, apiErr.ErrorCode)
	}
	return fmt.Errorf("Connect REST %s %s returned HTTP %d (response body suppressed)", method, path, status)
}

func normalizeConfig(raw map[string]any) (map[string]string, error) {
	out := make(map[string]string, len(raw))
	for key, value := range raw {
		switch v := value.(type) {
		case string:
			out[key] = v
		case json.Number:
			out[key] = v.String()
		case float64:
			out[key] = fmt.Sprintf("%v", v)
		case bool:
			out[key] = fmt.Sprintf("%t", v)
		case nil:
			return nil, fmt.Errorf("configuration key %q has null value", key)
		default:
			data, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			out[key] = string(data)
		}
	}
	return out, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
