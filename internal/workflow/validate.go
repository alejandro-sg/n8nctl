package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/alejandro-sg/n8nctl/pkg/n8n"
)

var (
	secretKeyPattern = regexp.MustCompile(`(?i)(password|passphrase|token|secret|api[_-]?key|client[_-]?secret|access[_-]?key|private[_-]?key)`)
	urlPattern       = regexp.MustCompile(`https?://[^\s"'<>]+`)
)

type ValidationOptions struct {
	EnvironmentName      string
	ProjectName          string
	ProductionHosts      []string
	AllowActive          bool
	RuntimeEngine        string
	RequireRemoteContext bool
}

type Finding struct {
	Severity       string   `json:"severity"`
	Code           string   `json:"code"`
	Message        string   `json:"message"`
	Path           string   `json:"path,omitempty"`
	NodeName       string   `json:"nodeName,omitempty"`
	NodeID         string   `json:"nodeId,omitempty"`
	Source         string   `json:"source,omitempty"`
	Remediation    string   `json:"remediation,omitempty"`
	CredentialName string   `json:"credentialName,omitempty"`
	CredentialID   string   `json:"credentialId,omitempty"`
	ExpectedTypes  []string `json:"expectedTypes,omitempty"`
	ActualType     string   `json:"actualType,omitempty"`
}

type ValidationResult struct {
	File                 string    `json:"file,omitempty"`
	WorkflowName         string    `json:"workflowName,omitempty"`
	NodeCount            int       `json:"nodeCount"`
	ConnectionCount      int       `json:"connectionCount"`
	CredentialReferences int       `json:"credentialReferences"`
	Findings             []Finding `json:"findings,omitempty"`
}

func LoadFile(path string) (*n8n.Workflow, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var workflow n8n.Workflow
	if err := json.Unmarshal(contents, &workflow); err != nil {
		return nil, err
	}

	return &workflow, nil
}

func ValidateFile(path string, opts ValidationOptions) (*n8n.Workflow, ValidationResult, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	workflow, err := LoadFile(path)
	if err != nil {
		return nil, ValidationResult{
			File: absPath,
			Findings: []Finding{{
				Severity: "error",
				Code:     "invalid_json",
				Message:  fmt.Sprintf("failed to parse workflow JSON: %v", err),
				Path:     absPath,
			}},
		}, nil
	}

	result := Validate(*workflow, opts)
	if opts.RuntimeEngine == "" || opts.RuntimeEngine == "n8n-runtime" {
		runtimeFindings, err := ValidateWithRuntime(path)
		if err != nil {
			result.addWarning("runtime_validator_unavailable", fmt.Sprintf("n8n runtime validator was unavailable: %v", err), "")
		} else {
			result.Findings = append(result.Findings, runtimeFindings...)
		}
	}
	result.File = absPath
	return workflow, result, nil
}

func Validate(workflow n8n.Workflow, opts ValidationOptions) ValidationResult {
	result := ValidationResult{
		WorkflowName:         strings.TrimSpace(workflow.Name),
		NodeCount:            len(workflow.Nodes),
		ConnectionCount:      countConnections(workflow.Connections),
		CredentialReferences: countCredentialReferences(workflow.Nodes),
	}

	if result.WorkflowName == "" {
		result.addError("missing_name", "workflow is missing a non-empty name", "name")
	}
	if len(workflow.Nodes) == 0 {
		result.addError("missing_nodes", "workflow must include at least one node", "nodes")
	}
	if workflow.Connections == nil {
		result.addError("missing_connections", "workflow is missing connections", "connections")
	}
	if workflow.Settings == nil {
		result.addError("missing_settings", "workflow is missing settings", "settings")
	}
	if workflow.Active && !opts.AllowActive {
		result.addError("active_not_allowed", "workflow JSON sets active=true; deploy activation must be explicit", "active")
	}

	seenNames := make(map[string]int, len(workflow.Nodes))
	for i, node := range workflow.Nodes {
		nodePath := fmt.Sprintf("nodes[%d]", i)
		name := strings.TrimSpace(node.Name)
		if name == "" {
			result.addError("missing_node_name", "node is missing a name", nodePath+".name")
		} else {
			seenNames[name]++
		}
		for credentialName, ref := range node.Credentials {
			if ref.ID.IsZero() && strings.TrimSpace(ref.Name) == "" {
				result.addWarning(
					"suspicious_credential_reference",
					fmt.Sprintf("node %q has credential %q without an id or name", node.Name, credentialName),
					nodePath+".credentials."+credentialName,
				)
			}
		}
	}
	for name, count := range seenNames {
		if count > 1 {
			result.addError("duplicate_node_name", fmt.Sprintf("node name %q is duplicated", name), "nodes")
		}
	}
	for sourceName, connectionValue := range workflow.Connections {
		if _, ok := seenNames[sourceName]; !ok {
			result.addError("connection_from_missing_node", fmt.Sprintf("connections reference missing source node %q", sourceName), "connections."+sourceName)
		}
		walkConnectionTargets(connectionValue, func(targetName string) {
			if _, ok := seenNames[targetName]; !ok {
				result.addError("connection_to_missing_node", fmt.Sprintf("connections reference missing target node %q", targetName), "connections."+sourceName)
			}
		})
	}

	generic, err := ToGenericMap(workflow)
	if err != nil {
		result.addWarning("internal_normalization_error", fmt.Sprintf("workflow could not be converted for deep inspection: %v", err), "")
	} else {
		walkValues(generic, "", func(path string, key string, value string) {
			lowerKey := strings.ToLower(key)
			if malformedTemplate(value) {
				result.addError("malformed_placeholder", fmt.Sprintf("malformed placeholder or expression syntax in %q", value), path)
			}
			if secretKeyPattern.MatchString(lowerKey) && looksHardcodedSecret(value) {
				result.addWarning("hardcoded_secret", fmt.Sprintf("possible hardcoded secret at %s", path), path)
			}
			if opts.EnvironmentName != "" && !strings.Contains(strings.ToLower(opts.EnvironmentName), "prod") {
				for _, host := range opts.ProductionHosts {
					if host != "" && strings.Contains(value, host) {
						result.addWarning("production_url_in_nonprod", fmt.Sprintf("value references production host %q", host), path)
						break
					}
				}
				if urlPattern.MatchString(value) && strings.Contains(strings.ToLower(value), "prod") {
					result.addWarning("production_url_in_nonprod", "value looks like a production URL", path)
				}
			}
		})
	}

	return result
}

func (r ValidationResult) HasErrors() bool {
	for _, finding := range r.Findings {
		if finding.Severity == "error" {
			return true
		}
	}
	return false
}

func (r ValidationResult) Errors() []Finding {
	items := make([]Finding, 0)
	for _, finding := range r.Findings {
		if finding.Severity == "error" {
			items = append(items, finding)
		}
	}
	return items
}

func (r ValidationResult) Warnings() []Finding {
	items := make([]Finding, 0)
	for _, finding := range r.Findings {
		if finding.Severity == "warning" {
			items = append(items, finding)
		}
	}
	return items
}

func (r *ValidationResult) addError(code, message, path string) {
	r.Findings = append(r.Findings, Finding{
		Severity: "error",
		Code:     code,
		Message:  message,
		Path:     path,
		Source:   "go",
	})
}

func (r *ValidationResult) addWarning(code, message, path string) {
	r.Findings = append(r.Findings, Finding{
		Severity: "warning",
		Code:     code,
		Message:  message,
		Path:     path,
		Source:   "go",
	})
}

func countCredentialReferences(nodes []n8n.Node) int {
	total := 0
	for _, node := range nodes {
		total += len(node.Credentials)
	}
	return total
}

func countConnections(connections map[string]any) int {
	total := 0
	var visit func(value any)
	visit = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			if _, ok := typed["node"]; ok {
				if _, ok := typed["type"]; ok {
					total++
				}
			}
			for _, child := range typed {
				visit(child)
			}
		case []any:
			for _, child := range typed {
				visit(child)
			}
		}
	}
	visit(connections)
	return total
}

func walkConnectionTargets(value any, visit func(targetName string)) {
	switch typed := value.(type) {
	case map[string]any:
		if node, ok := typed["node"].(string); ok && node != "" {
			visit(node)
		}
		for _, child := range typed {
			walkConnectionTargets(child, visit)
		}
	case []any:
		for _, child := range typed {
			walkConnectionTargets(child, visit)
		}
	}
}

func walkValues(value any, currentPath string, visit func(path string, key string, value string)) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			path := key
			if currentPath != "" {
				path = currentPath + "." + key
			}
			switch v := child.(type) {
			case string:
				visit(path, key, v)
			default:
				walkValues(child, path, visit)
			}
		}
	case []any:
		for i, child := range typed {
			path := fmt.Sprintf("%s[%d]", currentPath, i)
			if currentPath == "" {
				path = fmt.Sprintf("[%d]", i)
			}
			switch v := child.(type) {
			case string:
				visit(path, "", v)
			default:
				walkValues(child, path, visit)
			}
		}
	}
}

func malformedTemplate(value string) bool {
	left := strings.Count(value, "{{")
	right := strings.Count(value, "}}")
	return left != right
}

func looksHardcodedSecret(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "={{") || strings.Contains(trimmed, "{{") {
		return false
	}
	if strings.HasPrefix(trimmed, "$") {
		return false
	}
	return len(trimmed) >= 8
}
