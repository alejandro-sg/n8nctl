package api

import (
	"context"
	"net/url"
	"strconv"

	"github.com/LogicMonitor-IT/n8nctl/pkg/n8n"
)

type ListProjectsParams struct {
	Limit int
}

func (c *Client) ListProjects(ctx context.Context, params ListProjectsParams) ([]n8n.Project, error) {
	remaining := params.Limit
	cursor := ""
	projects := make([]n8n.Project, 0)

	for {
		pageSize := remaining
		if pageSize <= 0 || pageSize > 250 {
			pageSize = 250
		}
		page, err := c.listProjectsPage(ctx, cursor, pageSize)
		if err != nil {
			return nil, err
		}
		projects = append(projects, page.Data...)
		if params.Limit > 0 {
			remaining -= len(page.Data)
			if remaining <= 0 {
				return projects[:params.Limit], nil
			}
		}
		if page.NextCursor == "" || len(page.Data) == 0 {
			break
		}
		cursor = page.NextCursor
	}

	return projects, nil
}

func (c *Client) listProjectsPage(ctx context.Context, cursor string, limit int) (*n8n.ProjectListResponse, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		query.Set("cursor", cursor)
	}

	var response n8n.ProjectListResponse
	if err := c.doJSON(ctx, "GET", "/projects", query, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}
