package workflow

import (
	"encoding/json"

	"github.com/LogicMonitor-IT/n8nctl/pkg/n8n"
)

func PrepareForWrite(workflow n8n.Workflow) n8n.Workflow {
	prepared := workflow
	prepared.ID = ""
	prepared.ProjectID = ""
	prepared.Active = false
	prepared.CreatedAt = nil
	prepared.UpdatedAt = nil
	prepared.IsArchived = false
	prepared.VersionID = ""
	prepared.TriggerCount = 0
	prepared.Tags = nil
	prepared.Shared = nil
	prepared.ActiveVersion = nil
	prepared.StaticData = nil
	prepared.PinData = nil
	prepared.Meta = nil

	nodes := make([]n8n.Node, 0, len(prepared.Nodes))
	for _, node := range prepared.Nodes {
		node.WebhookID = ""
		nodes = append(nodes, node)
	}
	prepared.Nodes = nodes

	return prepared
}

func NormalizeForDiff(workflow n8n.Workflow) (map[string]any, error) {
	prepared := PrepareForWrite(workflow)
	return ToGenericMap(prepared)
}

func ToGenericMap(workflow n8n.Workflow) (map[string]any, error) {
	payload, err := json.Marshal(workflow)
	if err != nil {
		return nil, err
	}

	var generic map[string]any
	if err := json.Unmarshal(payload, &generic); err != nil {
		return nil, err
	}

	return generic, nil
}
