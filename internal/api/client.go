package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

type APIError struct {
	StatusCode int
	Method     string
	URL        string
	Message    string
	Body       string
}

func (e *APIError) Error() string {
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = strings.TrimSpace(e.Body)
	}
	if message == "" {
		message = fmt.Sprintf("%s %s returned status %d", e.Method, e.URL, e.StatusCode)
	}
	return message
}

func NewClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		HTTP:    httpClient,
	}
}

func (c *Client) doJSON(ctx context.Context, method, endpoint string, query url.Values, body any, out any) error {
	if c == nil {
		return fmt.Errorf("api client is nil")
	}
	if !AllowedEndpoint(method, endpoint) {
		return fmt.Errorf("n8n API endpoint denied by allowlist: %s %s", method, endpoint)
	}

	fullURL, err := url.Parse(c.BaseURL)
	if err != nil {
		return err
	}
	fullURL.Path = path.Join(fullURL.Path, "/api/v1", endpoint)
	if len(query) > 0 {
		fullURL.RawQuery = query.Encode()
	}

	var requestBody io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		requestBody = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL.String(), requestBody)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-N8N-API-KEY", c.APIKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{
			StatusCode: resp.StatusCode,
			Method:     method,
			URL:        fullURL.String(),
			Body:       strings.TrimSpace(string(responseBody)),
		}
		var structured struct {
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal(responseBody, &structured); err == nil {
			if structured.Message != "" {
				apiErr.Message = structured.Message
			} else {
				apiErr.Message = structured.Error
			}
		}
		return apiErr
	}

	if out == nil || len(responseBody) == 0 {
		return nil
	}

	if err := json.Unmarshal(responseBody, out); err != nil {
		return err
	}

	return nil
}

type endpointPattern struct {
	method   string
	segments []string
}

var endpointAllowlist = []endpointPattern{
	{method: http.MethodGet, segments: []string{"workflows"}},
	{method: http.MethodPost, segments: []string{"workflows"}},
	{method: http.MethodGet, segments: []string{"workflows", "{workflowId}"}},
	{method: http.MethodPut, segments: []string{"workflows", "{workflowId}"}},
	{method: http.MethodDelete, segments: []string{"workflows", "{workflowId}"}},
	{method: http.MethodPut, segments: []string{"workflows", "{workflowId}", "transfer"}},
	{method: http.MethodPost, segments: []string{"workflows", "{workflowId}", "run"}},
	{method: http.MethodPost, segments: []string{"workflows", "{workflowId}", "activate"}},
	{method: http.MethodPost, segments: []string{"workflows", "{workflowId}", "deactivate"}},
	{method: http.MethodGet, segments: []string{"executions"}},
	{method: http.MethodGet, segments: []string{"executions", "{executionId}"}},
	{method: http.MethodPost, segments: []string{"executions", "{executionId}", "retry"}},
	{method: http.MethodGet, segments: []string{"projects"}},
	{method: http.MethodGet, segments: []string{"credentials"}},
	{method: http.MethodGet, segments: []string{"credentials", "schema", "{credentialType}"}},
}

// AllowedEndpoint reports whether a method and public /api/v1-relative endpoint
// are intentionally supported by n8nctl.
func AllowedEndpoint(method, endpoint string) bool {
	cleanMethod := strings.ToUpper(strings.TrimSpace(method))
	cleanEndpoint := strings.TrimSpace(endpoint)
	cleanEndpoint = strings.TrimPrefix(cleanEndpoint, "/api/v1")
	cleanEndpoint = strings.Trim(cleanEndpoint, "/")
	if cleanEndpoint == "" {
		return false
	}
	segments := strings.Split(cleanEndpoint, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}

	for _, pattern := range endpointAllowlist {
		if pattern.method != cleanMethod || len(pattern.segments) != len(segments) {
			continue
		}
		if endpointSegmentsMatch(pattern.segments, segments) {
			return true
		}
	}
	return false
}

func endpointSegmentsMatch(pattern []string, actual []string) bool {
	for i := range pattern {
		if strings.HasPrefix(pattern[i], "{") && strings.HasSuffix(pattern[i], "}") {
			if strings.TrimSpace(actual[i]) == "" {
				return false
			}
			continue
		}
		if pattern[i] != actual[i] {
			return false
		}
	}
	return true
}
