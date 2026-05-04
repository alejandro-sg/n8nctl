package credential

import (
	"fmt"
	"strings"

	"github.com/LogicMonitor-IT/n8nctl/internal/workflow"
	"github.com/LogicMonitor-IT/n8nctl/pkg/n8n"
)

type LintOptions struct {
	ProjectID   string
	ProjectName string
}

type Result struct {
	Findings   []workflow.Finding `json:"findings,omitempty"`
	References []ReferenceCheck   `json:"references,omitempty"`
	Checked    int                `json:"checked"`
	Mode       string             `json:"mode,omitempty"`
	Skipped    bool               `json:"skipped,omitempty"`
}

func (r Result) HasErrors() bool {
	for _, finding := range r.Findings {
		if finding.Severity == "error" {
			return true
		}
	}
	return false
}

type ReferenceCheck struct {
	Status              string   `json:"status"`
	Severity            string   `json:"severity,omitempty"`
	NodeName            string   `json:"nodeName,omitempty"`
	NodeID              string   `json:"nodeId,omitempty"`
	NodeType            string   `json:"nodeType,omitempty"`
	CredentialKey       string   `json:"credentialKey,omitempty"`
	CredentialName      string   `json:"credentialName,omitempty"`
	CredentialID        string   `json:"credentialId,omitempty"`
	ExpectedTypes       []string `json:"expectedTypes,omitempty"`
	ActualType          string   `json:"actualType,omitempty"`
	ProjectStatus       string   `json:"projectStatus,omitempty"`
	TargetProjectID     string   `json:"targetProjectId,omitempty"`
	TargetProjectName   string   `json:"targetProjectName,omitempty"`
	CredentialProjectID string   `json:"credentialProjectId,omitempty"`
	Message             string   `json:"message,omitempty"`
	Remediation         string   `json:"remediation,omitempty"`
}

func (r *Result) DowngradeErrors() {
	for i := range r.Findings {
		if r.Findings[i].Severity == "error" {
			r.Findings[i].Severity = "warning"
		}
	}
	for i := range r.References {
		if r.References[i].Severity == "error" {
			r.References[i].Severity = "warning"
		}
	}
}

func Lint(workflowDoc n8n.Workflow, credentials []n8n.Credential, opts LintOptions) Result {
	index := indexCredentials(credentials)
	result := Result{}

	for i, node := range workflowDoc.Nodes {
		nodePath := fmt.Sprintf("nodes[%d]", i)
		requiredFindings, requiredChecks := requiredCredentialFindings(node, nodePath)
		result.Findings = append(result.Findings, requiredFindings...)
		result.References = append(result.References, requiredChecks...)
		result.Findings = append(result.Findings, httpGoogleFindings(node, nodePath)...)

		for credentialKey, ref := range node.Credentials {
			result.Checked++
			findings, checks := resolveCredential(node, nodePath, credentialKey, ref, index, opts)
			result.Findings = append(result.Findings, findings...)
			result.References = append(result.References, checks...)
		}
	}

	return result
}

type credentialIndex struct {
	byID   map[string]n8n.Credential
	byName map[string][]n8n.Credential
}

func indexCredentials(credentials []n8n.Credential) credentialIndex {
	index := credentialIndex{
		byID:   make(map[string]n8n.Credential, len(credentials)),
		byName: make(map[string][]n8n.Credential),
	}
	for _, credential := range credentials {
		if id := credential.ID.String(); id != "" {
			index.byID[id] = credential
		}
		if credential.Name != "" {
			index.byName[credential.Name] = append(index.byName[credential.Name], credential)
		}
	}
	return index
}

func resolveCredential(node n8n.Node, nodePath string, credentialKey string, ref n8n.CredentialReference, index credentialIndex, opts LintOptions) ([]workflow.Finding, []ReferenceCheck) {
	findings := make([]workflow.Finding, 0)
	check := baseCheck(node, credentialKey, ref, opts)
	rule := credentialRuleFor(node, credentialKey)
	check.ExpectedTypes = append([]string(nil), rule.AcceptedTypes...)

	if ref.ID.IsZero() && strings.TrimSpace(ref.Name) == "" {
		check.Status = "missing_reference"
		check.Severity = "error"
		check.Message = fmt.Sprintf("node %q has credential %q without id or name", node.Name, credentialKey)
		check.Remediation = "Bind this node to an existing credential."
		return []workflow.Finding{newCredentialFinding("error", "missing_credential_reference", check.Message, nodePath+".credentials."+credentialKey, node, ref, rule.AcceptedTypes, "", "Bind this node to an existing credential.")}, []ReferenceCheck{check}
	}

	var matches []n8n.Credential
	if id := ref.ID.String(); id != "" {
		if credential, ok := index.byID[id]; ok {
			matches = append(matches, credential)
		}
	}
	if len(matches) == 0 && ref.Name != "" {
		matches = append(matches, index.byName[ref.Name]...)
	}

	if len(matches) == 0 {
		label := ref.Name
		if label == "" {
			label = ref.ID.String()
		}
		check.Status = "not_found"
		check.Severity = "error"
		check.Message = fmt.Sprintf("credential %q referenced by node %q was not found", label, node.Name)
		check.Remediation = "Create or share the credential into the target project, then rebind the node."
		return []workflow.Finding{newCredentialFinding("error", "credential_not_found", check.Message, nodePath+".credentials."+credentialKey, node, ref, rule.AcceptedTypes, "", check.Remediation)}, []ReferenceCheck{check}
	}
	if len(matches) > 1 && ref.ID.IsZero() {
		check.Status = "ambiguous"
		check.Severity = "error"
		check.Message = fmt.Sprintf("credential name %q referenced by node %q matched multiple credentials", ref.Name, node.Name)
		check.Remediation = "Rebind by credential id or rename credentials so the name is unique."
		return []workflow.Finding{newCredentialFinding("error", "credential_name_ambiguous", check.Message, nodePath+".credentials."+credentialKey, node, ref, rule.AcceptedTypes, "", check.Remediation)}, []ReferenceCheck{check}
	}

	for _, credential := range matches {
		check.CredentialID = credential.ID.String()
		check.CredentialName = credential.Name
		check.ActualType = credential.Type
		check.CredentialProjectID = credential.ProjectID.String()

		if finding := typeFinding(node, nodePath, credentialKey, ref, credential, rule, &check); finding != nil {
			findings = append(findings, *finding)
		}
		if opts.ProjectID != "" && credentialHasProjectMetadata(credential) && !credentialInProject(credential, opts.ProjectID) {
			check.Status = "not_shared_with_project"
			check.Severity = "error"
			check.ProjectStatus = "not_shared"
			check.Message = fmt.Sprintf("credential %q is not available in project %q", credential.Name, projectLabel(opts))
			check.Remediation = "Share or move the credential into the target project before deploying."
			findings = append(findings, newCredentialFinding("error", "credential_wrong_project", check.Message, nodePath+".credentials."+credentialKey, node, ref, rule.AcceptedTypes, credential.Type, check.Remediation))
		} else if opts.ProjectID != "" && !credentialHasProjectMetadata(credential) {
			if check.Status == "" || check.Status == "ok" || check.Status == "valid_alternative_type" {
				check.Status = "unable_to_verify"
				check.Severity = "warning"
				check.ProjectStatus = "unknown"
				check.Message = fmt.Sprintf("credential %q project sharing could not be verified because the API response did not include project metadata", credential.Name)
				check.Remediation = "Verify the credential is shared with the target project in n8n."
				findings = append(findings, newCredentialFinding("warning", "credential_project_unverified", check.Message, nodePath+".credentials."+credentialKey, node, ref, rule.AcceptedTypes, credential.Type, check.Remediation))
			}
		} else if opts.ProjectID != "" {
			check.ProjectStatus = "shared"
		}
		if check.Status == "" {
			check.Status = "ok"
			check.Severity = "info"
			check.Message = fmt.Sprintf("credential %q is available for node %q", credential.Name, node.Name)
		}
	}

	return findings, []ReferenceCheck{check}
}

func typeFinding(node n8n.Node, nodePath string, credentialKey string, ref n8n.CredentialReference, credential n8n.Credential, rule credentialRule, check *ReferenceCheck) *workflow.Finding {
	actualType := credential.Type
	switch {
	case actualType == "":
		check.Status = "unable_to_verify"
		check.Severity = "warning"
		check.Message = fmt.Sprintf("credential %q type could not be verified because the API response did not include a type", credential.Name)
		check.Remediation = "Verify the credential type in n8n if this preflight result is unexpected."
		finding := newCredentialFinding("warning", "credential_type_unverified", check.Message, nodePath+".credentials."+credentialKey, node, ref, rule.AcceptedTypes, actualType, check.Remediation)
		return &finding
	case len(rule.AcceptedTypes) == 0:
		if actualType == credentialKey {
			check.Status = "ok"
			check.Severity = "info"
			return nil
		}
		check.Status = "unable_to_verify"
		check.Severity = "warning"
		check.Message = fmt.Sprintf("node %q references credential %q of type %q but accepted credential types could not be verified", node.Name, credential.Name, actualType)
		check.Remediation = "Verify the node credential type in the installed n8n editor, or update n8nctl metadata if this is a common node."
		finding := newCredentialFinding("warning", "credential_type_unverified", check.Message, nodePath+".credentials."+credentialKey, node, ref, rule.AcceptedTypes, actualType, check.Remediation)
		return &finding
	case containsString(rule.AcceptedTypes, actualType):
		if actualType == credentialKey {
			check.Status = "ok"
			check.Severity = "info"
		} else {
			check.Status = "valid_alternative_type"
			check.Severity = "info"
			check.Message = fmt.Sprintf("credential %q uses valid alternative type %q for node %q", credential.Name, actualType, node.Name)
		}
		return nil
	case !rule.Known:
		check.Status = "unable_to_verify"
		check.Severity = "warning"
		check.Message = fmt.Sprintf("node %q credential type could not be verified against installed n8n metadata", node.Name)
		check.Remediation = "Verify the credential binding in n8n before deploying."
		finding := newCredentialFinding("warning", "credential_type_unverified", check.Message, nodePath+".credentials."+credentialKey, node, ref, rule.AcceptedTypes, actualType, check.Remediation)
		return &finding
	default:
		check.Status = "wrong_type"
		check.Severity = "error"
		check.Message = fmt.Sprintf("node %q references credential %q of type %q but accepted type(s) are %s", node.Name, credential.Name, actualType, strings.Join(rule.AcceptedTypes, ", "))
		check.Remediation = "Rebind the node to a credential with an accepted n8n credential type."
		finding := newCredentialFinding("error", "wrong_credential_type", check.Message, nodePath+".credentials."+credentialKey, node, ref, rule.AcceptedTypes, actualType, check.Remediation)
		return &finding
	}
}

func requiredCredentialFindings(node n8n.Node, nodePath string) ([]workflow.Finding, []ReferenceCheck) {
	required := requiredCredentialRules(node)
	findings := make([]workflow.Finding, 0, len(required))
	checks := make([]ReferenceCheck, 0, len(required))
	for _, rule := range required {
		if len(rule.AcceptedTypes) == 0 {
			continue
		}
		key := rule.AcceptedTypes[0]
		ref, ok := node.Credentials[key]
		if (!ok || (ref.ID.IsZero() && strings.TrimSpace(ref.Name) == "")) && !hasAnyCredentialReference(node) {
			message := fmt.Sprintf("node %q is missing required credential %q", node.Name, key)
			remediation := "Bind the required credential before deploying or running the workflow."
			findings = append(findings, newCredentialFinding("error", "missing_required_credential", message, nodePath+".credentials."+key, node, ref, rule.AcceptedTypes, "", remediation))
			check := baseCheck(node, key, ref, LintOptions{})
			check.Status = "missing_reference"
			check.Severity = "error"
			check.ExpectedTypes = rule.AcceptedTypes
			check.Message = message
			check.Remediation = remediation
			checks = append(checks, check)
		}
	}
	return findings, checks
}

func hasAnyCredentialReference(node n8n.Node) bool {
	for _, ref := range node.Credentials {
		if !ref.ID.IsZero() || strings.TrimSpace(ref.Name) != "" {
			return true
		}
	}
	return false
}

func httpGoogleFindings(node n8n.Node, nodePath string) []workflow.Finding {
	if node.Type != "n8n-nodes-base.httpRequest" {
		return nil
	}
	hasGoogle := false
	for key := range node.Credentials {
		if strings.Contains(strings.ToLower(key), "google") {
			hasGoogle = true
			break
		}
	}
	if !hasGoogle {
		return nil
	}
	authMode := strings.TrimSpace(asString(node.Parameters["authentication"]))
	genericAuthType := strings.TrimSpace(asString(node.Parameters["genericAuthType"]))
	if authMode == "" || authMode == "none" {
		return []workflow.Finding{newFinding("warning", "http_google_auth_mode", fmt.Sprintf("HTTP Request node %q references Google credentials but authentication is not enabled", node.Name), nodePath+".parameters.authentication", node, "Use predefined or generic credential auth for Google APIs, then verify OAuth scopes in the credential.")}
	}
	if authMode == "genericCredentialType" && !strings.Contains(strings.ToLower(genericAuthType), "oauth") {
		return []workflow.Finding{newFinding("warning", "http_google_auth_mode", fmt.Sprintf("HTTP Request node %q references Google credentials without OAuth generic auth", node.Name), nodePath+".parameters.genericAuthType", node, "Use OAuth2-based generic authentication unless the endpoint expects another scheme.")}
	}
	return nil
}

func credentialInProject(credential n8n.Credential, projectID string) bool {
	if credential.ProjectID.String() == projectID {
		return true
	}
	for _, shared := range credential.Shared {
		if shared.ProjectID.String() == projectID {
			return true
		}
		if id, ok := shared.Project["id"].(string); ok && id == projectID {
			return true
		}
	}
	return false
}

func credentialHasProjectMetadata(credential n8n.Credential) bool {
	return credential.ProjectID.String() != "" || len(credential.Shared) > 0
}

func projectLabel(opts LintOptions) string {
	if opts.ProjectName != "" {
		return opts.ProjectName
	}
	return opts.ProjectID
}

func newFinding(severity, code, message, path string, node n8n.Node, remediation string) workflow.Finding {
	return workflow.Finding{
		Severity:    severity,
		Code:        code,
		Message:     message,
		Path:        path,
		NodeName:    node.Name,
		NodeID:      node.ID.String(),
		Source:      "credential",
		Remediation: remediation,
	}
}

func newCredentialFinding(severity, code, message, path string, node n8n.Node, ref n8n.CredentialReference, expectedTypes []string, actualType string, remediation string) workflow.Finding {
	finding := newFinding(severity, code, message, path, node, remediation)
	finding.CredentialName = ref.Name
	finding.CredentialID = ref.ID.String()
	finding.ExpectedTypes = append([]string(nil), expectedTypes...)
	finding.ActualType = actualType
	return finding
}

func baseCheck(node n8n.Node, credentialKey string, ref n8n.CredentialReference, opts LintOptions) ReferenceCheck {
	return ReferenceCheck{
		NodeName:          node.Name,
		NodeID:            node.ID.String(),
		NodeType:          node.Type,
		CredentialKey:     credentialKey,
		CredentialName:    ref.Name,
		CredentialID:      ref.ID.String(),
		TargetProjectID:   opts.ProjectID,
		TargetProjectName: opts.ProjectName,
	}
}

func asString(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%v", value)
}
