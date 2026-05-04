package api

import (
	"context"
	"net/url"
	"strconv"

	"github.com/LogicMonitor-IT/n8nctl/pkg/n8n"
)

type ListExecutionsParams struct {
	Status      string
	WorkflowID  string
	ProjectID   string
	IncludeData bool
	Limit       int
}

func (c *Client) ListExecutions(ctx context.Context, params ListExecutionsParams) ([]n8n.Execution, error) {
	remaining := params.Limit
	cursor := ""
	executions := make([]n8n.Execution, 0)

	for {
		pageSize := remaining
		if pageSize <= 0 || pageSize > 250 {
			pageSize = 250
		}

		page, err := c.listExecutionsPage(ctx, params, cursor, pageSize)
		if err != nil {
			return nil, err
		}

		executions = append(executions, page.Data...)
		if params.Limit > 0 {
			remaining -= len(page.Data)
			if remaining <= 0 {
				return executions[:params.Limit], nil
			}
		}
		if page.NextCursor == "" || len(page.Data) == 0 {
			break
		}
		cursor = page.NextCursor
	}

	return executions, nil
}

func (c *Client) listExecutionsPage(ctx context.Context, params ListExecutionsParams, cursor string, limit int) (*n8n.ExecutionListResponse, error) {
	query := url.Values{}
	if params.Status != "" {
		query.Set("status", params.Status)
	}
	if params.WorkflowID != "" {
		query.Set("workflowId", params.WorkflowID)
	}
	if params.ProjectID != "" {
		query.Set("projectId", params.ProjectID)
	}
	if params.IncludeData {
		query.Set("includeData", "true")
	}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		query.Set("cursor", cursor)
	}

	var response n8n.ExecutionListResponse
	if err := c.doJSON(ctx, "GET", "/executions", query, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) GetExecution(ctx context.Context, executionID string, includeData bool) (*n8n.Execution, error) {
	query := url.Values{}
	if includeData {
		query.Set("includeData", "true")
	}

	var execution n8n.Execution
	if err := c.doJSON(ctx, "GET", "/executions/"+url.PathEscape(executionID), query, nil, &execution); err != nil {
		return nil, err
	}
	return &execution, nil
}

func (c *Client) RetryExecution(ctx context.Context, executionID string, loadWorkflow bool) (*n8n.Execution, error) {
	body := map[string]any{}
	if loadWorkflow {
		body["loadWorkflow"] = true
	}

	var execution n8n.Execution
	if err := c.doJSON(ctx, "POST", "/executions/"+url.PathEscape(executionID)+"/retry", nil, body, &execution); err != nil {
		return nil, err
	}
	return &execution, nil
}
