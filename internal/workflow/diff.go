package workflow

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/go-cmp/cmp"

	"github.com/LogicMonitor-IT/n8nctl/pkg/n8n"
)

type DiffResult struct {
	Equal      bool           `json:"equal"`
	Diff       string         `json:"diff,omitempty"`
	Changes    []DiffChange   `json:"changes,omitempty"`
	Local      map[string]any `json:"local,omitempty"`
	Remote     map[string]any `json:"remote,omitempty"`
	WorkflowID string         `json:"workflowId,omitempty"`
}

type DiffChange struct {
	Category string `json:"category"`
	Name     string `json:"name,omitempty"`
	Field    string `json:"field,omitempty"`
	Change   string `json:"change"`
}

func Diff(local, remote n8n.Workflow) (*DiffResult, error) {
	localNormalized, err := NormalizeForDiff(local)
	if err != nil {
		return nil, err
	}
	remoteNormalized, err := NormalizeForDiff(remote)
	if err != nil {
		return nil, err
	}

	return &DiffResult{
		Equal:   cmp.Equal(localNormalized, remoteNormalized),
		Diff:    cmp.Diff(remoteNormalized, localNormalized),
		Changes: GroupedChanges(local, remote),
		Local:   localNormalized,
		Remote:  remoteNormalized,
	}, nil
}

func GroupedChanges(local, remote n8n.Workflow) []DiffChange {
	local = PrepareForWrite(local)
	remote = PrepareForWrite(remote)
	changes := make([]DiffChange, 0)

	if local.Name != remote.Name {
		changes = append(changes, DiffChange{Category: "workflow", Field: "name", Change: fmt.Sprintf("%q -> %q", remote.Name, local.Name)})
	}
	if !genericEqual(local.Settings, remote.Settings) {
		changes = append(changes, DiffChange{Category: "workflow", Field: "settings", Change: "changed"})
	}

	localNodes := nodesByName(local.Nodes)
	remoteNodes := nodesByName(remote.Nodes)
	for _, name := range sortedUnion(localNodes, remoteNodes) {
		localNode, localOK := localNodes[name]
		remoteNode, remoteOK := remoteNodes[name]
		switch {
		case !remoteOK:
			changes = append(changes, DiffChange{Category: "node", Name: name, Change: "added"})
		case !localOK:
			changes = append(changes, DiffChange{Category: "node", Name: name, Change: "removed"})
		default:
			changes = append(changes, nodeChanges(localNode, remoteNode)...)
		}
	}

	for _, name := range sortedMapUnion(local.Connections, remote.Connections) {
		localValue, localOK := local.Connections[name]
		remoteValue, remoteOK := remote.Connections[name]
		switch {
		case !remoteOK:
			changes = append(changes, DiffChange{Category: "connection", Name: name, Change: "added"})
		case !localOK:
			changes = append(changes, DiffChange{Category: "connection", Name: name, Change: "removed"})
		case !genericEqual(localValue, remoteValue):
			changes = append(changes, DiffChange{Category: "connection", Name: name, Change: "changed"})
		}
	}

	return changes
}

func nodeChanges(local, remote n8n.Node) []DiffChange {
	changes := make([]DiffChange, 0)
	if local.Type != remote.Type {
		changes = append(changes, DiffChange{Category: "node", Name: local.Name, Field: "type", Change: fmt.Sprintf("%q -> %q", remote.Type, local.Type)})
	}
	if !genericEqual(local.TypeVersion, remote.TypeVersion) {
		changes = append(changes, DiffChange{Category: "node", Name: local.Name, Field: "typeVersion", Change: "changed"})
	}
	if local.Disabled != remote.Disabled {
		changes = append(changes, DiffChange{Category: "node", Name: local.Name, Field: "disabled", Change: fmt.Sprintf("%t -> %t", remote.Disabled, local.Disabled)})
	}
	if !genericEqual(local.Parameters, remote.Parameters) {
		changes = append(changes, DiffChange{Category: "node", Name: local.Name, Field: "parameters", Change: "changed"})
	}
	if !genericEqual(local.Credentials, remote.Credentials) {
		changes = append(changes, DiffChange{Category: "node", Name: local.Name, Field: "credentials", Change: "changed"})
	}
	return changes
}

func nodesByName(nodes []n8n.Node) map[string]n8n.Node {
	index := make(map[string]n8n.Node, len(nodes))
	for _, node := range nodes {
		index[node.Name] = node
	}
	return index
}

func sortedUnion(left, right map[string]n8n.Node) []string {
	values := make(map[string]struct{}, len(left)+len(right))
	for key := range left {
		values[key] = struct{}{}
	}
	for key := range right {
		values[key] = struct{}{}
	}
	return sortedKeys(values)
}

func sortedMapUnion(left, right map[string]any) []string {
	values := make(map[string]struct{}, len(left)+len(right))
	for key := range left {
		values[key] = struct{}{}
	}
	for key := range right {
		values[key] = struct{}{}
	}
	return sortedKeys(values)
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func genericEqual(left, right any) bool {
	return cmp.Equal(canonicalJSON(left), canonicalJSON(right))
}

func canonicalJSON(value any) any {
	payload, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var generic any
	if err := json.Unmarshal(payload, &generic); err != nil {
		return value
	}
	return generic
}
