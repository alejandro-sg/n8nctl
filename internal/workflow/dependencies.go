package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/LogicMonitor-IT/n8nctl/pkg/n8n"
)

func Dependencies(workflow n8n.Workflow) []n8n.WorkflowDependency {
	dependencies := make([]n8n.WorkflowDependency, 0)

	for _, node := range workflow.Nodes {
		for credentialType, ref := range node.Credentials {
			dependencies = append(dependencies, n8n.WorkflowDependency{
				Type:     "credential",
				Name:     firstNonEmpty(ref.Name, credentialType),
				ID:       ref.ID.String(),
				NodeName: node.Name,
				Detail:   credentialType,
			})
		}

		if strings.Contains(strings.ToLower(node.Type), "executeworkflow") {
			if id := parameterString(node.Parameters, "workflowId"); id != "" {
				dependencies = append(dependencies, n8n.WorkflowDependency{
					Type:     "subworkflow",
					ID:       id,
					NodeName: node.Name,
				})
			}
		}

		if strings.Contains(strings.ToLower(node.Type), "webhook") || node.WebhookID != "" {
			dependencies = append(dependencies, n8n.WorkflowDependency{
				Type:     "webhook",
				ID:       node.WebhookID,
				NodeName: node.Name,
				Detail:   node.Type,
			})
		}

		if node.Type == "n8n-nodes-base.httpRequest" {
			if url := parameterString(node.Parameters, "url"); url != "" {
				dependencies = append(dependencies, n8n.WorkflowDependency{
					Type:     "external_endpoint",
					Name:     url,
					NodeName: node.Name,
				})
			}
		}

		walkParameterStrings(node.Parameters, func(path, value string) {
			lowerPath := strings.ToLower(path)
			switch {
			case strings.Contains(lowerPath, "datatable"), strings.Contains(lowerPath, "tableid"), strings.Contains(lowerPath, "tablename"):
				dependencies = append(dependencies, n8n.WorkflowDependency{
					Type:     "data_table",
					Name:     value,
					NodeName: node.Name,
					Detail:   path,
				})
			case strings.Contains(value, "$vars."):
				dependencies = append(dependencies, n8n.WorkflowDependency{
					Type:     "variable",
					Name:     value,
					NodeName: node.Name,
					Detail:   path,
				})
			case strings.Contains(value, "$env."):
				dependencies = append(dependencies, n8n.WorkflowDependency{
					Type:     "environment_variable",
					Name:     value,
					NodeName: node.Name,
					Detail:   path,
				})
			}
		})
	}

	return dedupeDependencies(dependencies)
}

func parameterString(parameters map[string]any, key string) string {
	if parameters == nil {
		return ""
	}
	value, ok := parameters[key]
	if !ok {
		return ""
	}
	return stringValue(value)
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		for _, key := range []string{"value", "id", "name"} {
			if child := stringValue(typed[key]); child != "" {
				return child
			}
		}
	}
	text := strings.TrimSpace(fmt.Sprintf("%v", value))
	if text == "<nil>" {
		return ""
	}
	return text
}

func walkParameterStrings(value any, visit func(path, value string)) {
	var walk func(any, string)
	walk = func(current any, path string) {
		switch typed := current.(type) {
		case map[string]any:
			for key, child := range typed {
				childPath := key
				if path != "" {
					childPath = path + "." + key
				}
				walk(child, childPath)
			}
		case []any:
			for i, child := range typed {
				walk(child, fmt.Sprintf("%s[%d]", path, i))
			}
		case string:
			if strings.TrimSpace(typed) != "" {
				visit(path, typed)
			}
		}
	}
	walk(value, "")
}

func dedupeDependencies(items []n8n.WorkflowDependency) []n8n.WorkflowDependency {
	seen := make(map[string]struct{}, len(items))
	deduped := make([]n8n.WorkflowDependency, 0, len(items))
	for _, item := range items {
		key := strings.Join([]string{item.Type, item.Name, item.ID, item.NodeName, item.Detail}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, item)
	}
	sort.Slice(deduped, func(i, j int) bool {
		left := strings.Join([]string{deduped[i].Type, deduped[i].NodeName, deduped[i].Name, deduped[i].ID}, "\x00")
		right := strings.Join([]string{deduped[j].Type, deduped[j].NodeName, deduped[j].Name, deduped[j].ID}, "\x00")
		return left < right
	})
	return deduped
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
