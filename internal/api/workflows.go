package api

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/LogicMonitor-IT/n8nctl/pkg/n8n"
)

type ListWorkflowsParams struct {
	Name              string
	Active            *bool
	ExcludePinnedData bool
	ProjectID         string
	Tags              []string
	Limit             int
}

func (c *Client) ListWorkflows(ctx context.Context, params ListWorkflowsParams) ([]n8n.Workflow, error) {
	remaining := params.Limit
	cursor := ""
	workflows := make([]n8n.Workflow, 0)

	for {
		pageSize := remaining
		if pageSize <= 0 || pageSize > 250 {
			pageSize = 250
		}

		page, err := c.listWorkflowsPage(ctx, params, cursor, pageSize)
		if err != nil {
			return nil, err
		}
		workflows = append(workflows, page.Data...)
		if params.Limit > 0 {
			remaining -= len(page.Data)
			if remaining <= 0 {
				return workflows[:params.Limit], nil
			}
		}
		if page.NextCursor == "" || len(page.Data) == 0 {
			break
		}
		cursor = page.NextCursor
	}

	return workflows, nil
}

func (c *Client) listWorkflowsPage(ctx context.Context, params ListWorkflowsParams, cursor string, limit int) (*n8n.WorkflowListResponse, error) {
	query := url.Values{}
	if params.Name != "" {
		query.Set("name", params.Name)
	}
	if params.Active != nil {
		query.Set("active", strconv.FormatBool(*params.Active))
	}
	if params.ExcludePinnedData {
		query.Set("excludePinnedData", "true")
	}
	if params.ProjectID != "" {
		query.Set("projectId", params.ProjectID)
	}
	if len(params.Tags) > 0 {
		query.Set("tags", strings.Join(params.Tags, ","))
	}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		query.Set("cursor", cursor)
	}

	var response n8n.WorkflowListResponse
	if err := c.doJSON(ctx, "GET", "/workflows", query, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) GetWorkflow(ctx context.Context, workflowID string, excludePinnedData bool) (*n8n.Workflow, error) {
	query := url.Values{}
	if excludePinnedData {
		query.Set("excludePinnedData", "true")
	}

	var workflow n8n.Workflow
	if err := c.doJSON(ctx, "GET", "/workflows/"+url.PathEscape(workflowID), query, nil, &workflow); err != nil {
		return nil, err
	}
	return &workflow, nil
}

func (c *Client) CreateWorkflow(ctx context.Context, workflow n8n.Workflow) (*n8n.Workflow, error) {
	var created n8n.Workflow
	payload, err := workflowWritePayload(workflow, true)
	if err != nil {
		return nil, err
	}
	if err := c.doJSON(ctx, "POST", "/workflows", nil, payload, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

func (c *Client) TransferWorkflow(ctx context.Context, workflowID string, destinationProjectID string, shareCredentials bool) error {
	body := map[string]any{
		"destinationProjectId": destinationProjectID,
		"shareCredentials":     shareCredentials,
	}
	return c.doJSON(ctx, "PUT", "/workflows/"+url.PathEscape(workflowID)+"/transfer", nil, body, nil)
}

type RunWorkflowRequest struct {
	WorkflowData    *n8n.Workflow  `json:"workflowData,omitempty"`
	StartNodes      []string       `json:"startNodes,omitempty"`
	DestinationNode string         `json:"destinationNode,omitempty"`
	RunData         map[string]any `json:"runData,omitempty"`
}

func (c *Client) RunWorkflow(ctx context.Context, workflowID string, request RunWorkflowRequest) (*n8n.ExecutionRunResponse, error) {
	var response n8n.ExecutionRunResponse
	if err := c.doJSON(ctx, "POST", "/workflows/"+url.PathEscape(workflowID)+"/run", nil, request, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) UpdateWorkflow(ctx context.Context, workflowID string, workflow n8n.Workflow) (*n8n.Workflow, error) {
	var updated n8n.Workflow
	payload, err := workflowWritePayload(workflow, false)
	if err != nil {
		return nil, err
	}
	if err := c.doJSON(ctx, "PUT", "/workflows/"+url.PathEscape(workflowID), nil, payload, &updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

func (c *Client) DeleteWorkflow(ctx context.Context, workflowID string) error {
	return c.doJSON(ctx, "DELETE", "/workflows/"+url.PathEscape(workflowID), nil, nil, nil)
}

func (c *Client) ActivateWorkflow(ctx context.Context, workflowID string) (*n8n.Workflow, error) {
	var updated n8n.Workflow
	if err := c.doJSON(ctx, "POST", "/workflows/"+url.PathEscape(workflowID)+"/activate", nil, nil, &updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

func (c *Client) DeactivateWorkflow(ctx context.Context, workflowID string) (*n8n.Workflow, error) {
	var updated n8n.Workflow
	if err := c.doJSON(ctx, "POST", "/workflows/"+url.PathEscape(workflowID)+"/deactivate", nil, nil, &updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

func workflowWritePayload(workflow n8n.Workflow, includeProjectID bool) (map[string]any, error) {
	encoded, err := json.Marshal(workflow)
	if err != nil {
		return nil, err
	}

	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return nil, err
	}

	for _, field := range workflowReadOnlyFields(includeProjectID) {
		delete(payload, field)
	}
	return payload, nil
}

func workflowReadOnlyFields(includeProjectID bool) []string {
	fields := []string{
		"active",
		"id",
		"createdAt",
		"updatedAt",
		"versionId",
		"versionCounter",
		"shared",
		"tags",
		"triggerCount",
		"activeVersion",
		"activeVersionId",
		"isArchived",
		"staticData",
		"pinData",
		"meta",
	}
	if !includeProjectID {
		fields = append(fields, "projectId")
	}
	return fields
}
