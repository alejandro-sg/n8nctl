package api

import (
	"context"
	"net/url"
	"strconv"

	"github.com/LogicMonitor-IT/n8nctl/pkg/n8n"
)

type ListCredentialsParams struct {
	Limit int
}

func (c *Client) ListCredentials(ctx context.Context, params ListCredentialsParams) ([]n8n.Credential, error) {
	remaining := params.Limit
	cursor := ""
	credentials := make([]n8n.Credential, 0)

	for {
		pageSize := remaining
		if pageSize <= 0 || pageSize > 250 {
			pageSize = 250
		}
		page, err := c.listCredentialsPage(ctx, cursor, pageSize)
		if err != nil {
			return nil, err
		}
		credentials = append(credentials, page.Data...)
		if params.Limit > 0 {
			remaining -= len(page.Data)
			if remaining <= 0 {
				return credentials[:params.Limit], nil
			}
		}
		if page.NextCursor == "" || len(page.Data) == 0 {
			break
		}
		cursor = page.NextCursor
	}

	return credentials, nil
}

func (c *Client) listCredentialsPage(ctx context.Context, cursor string, limit int) (*n8n.CredentialListResponse, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		query.Set("cursor", cursor)
	}

	var response n8n.CredentialListResponse
	if err := c.doJSON(ctx, "GET", "/credentials", query, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) GetCredentialSchema(ctx context.Context, credentialType string) (*n8n.CredentialSchema, error) {
	var schema n8n.CredentialSchema
	if err := c.doJSON(ctx, "GET", "/credentials/schema/"+url.PathEscape(credentialType), nil, nil, &schema); err != nil {
		return nil, err
	}
	return &schema, nil
}
