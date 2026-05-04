package credential

import (
	"testing"

	"github.com/LogicMonitor-IT/n8nctl/pkg/n8n"
)

func TestLintCredentialFindings(t *testing.T) {
	workflow := n8n.Workflow{
		Name: "Credential Lint",
		Nodes: []n8n.Node{
			{
				ID:   n8n.ID("node-1"),
				Name: "Drive",
				Type: "n8n-nodes-base.googleDrive",
			},
			{
				ID:   n8n.ID("node-2"),
				Name: "Wrong Type",
				Type: "n8n-nodes-base.slack",
				Credentials: map[string]n8n.CredentialReference{
					"slackApi": {ID: n8n.ID("cred-google"), Name: "Google"},
				},
			},
			{
				ID:   n8n.ID("node-3"),
				Name: "Missing Remote",
				Type: "n8n-nodes-base.slack",
				Credentials: map[string]n8n.CredentialReference{
					"slackApi": {ID: n8n.ID("missing"), Name: "Missing"},
				},
			},
			{
				ID:   n8n.ID("node-4"),
				Name: "Ambiguous",
				Type: "n8n-nodes-base.slack",
				Credentials: map[string]n8n.CredentialReference{
					"slackApi": {Name: "Shared Name"},
				},
			},
			{
				ID:   n8n.ID("node-5"),
				Name: "Wrong Project",
				Type: "n8n-nodes-base.slack",
				Credentials: map[string]n8n.CredentialReference{
					"slackApi": {ID: n8n.ID("cred-other"), Name: "Other Project"},
				},
			},
			{
				ID:   n8n.ID("node-6"),
				Name: "HTTP Google",
				Type: "n8n-nodes-base.httpRequest",
				Parameters: map[string]any{
					"authentication": "none",
				},
				Credentials: map[string]n8n.CredentialReference{
					"googleOAuth2Api": {ID: n8n.ID("cred-google"), Name: "Google"},
				},
			},
		},
	}
	credentials := []n8n.Credential{
		{ID: n8n.ID("cred-google"), Name: "Google", Type: "googleOAuth2Api", ProjectID: n8n.ID("proj-1")},
		{ID: n8n.ID("cred-shared-1"), Name: "Shared Name", Type: "slackApi", ProjectID: n8n.ID("proj-1")},
		{ID: n8n.ID("cred-shared-2"), Name: "Shared Name", Type: "slackApi", ProjectID: n8n.ID("proj-1")},
		{ID: n8n.ID("cred-other"), Name: "Other Project", Type: "slackApi", ProjectID: n8n.ID("proj-2")},
	}

	result := Lint(workflow, credentials, LintOptions{ProjectID: "proj-1", ProjectName: "Project A"})
	seen := map[string]bool{}
	for _, finding := range result.Findings {
		seen[finding.Code] = true
	}
	for _, code := range []string{
		"missing_required_credential",
		"wrong_credential_type",
		"credential_not_found",
		"credential_name_ambiguous",
		"credential_wrong_project",
		"http_google_auth_mode",
	} {
		if !seen[code] {
			t.Fatalf("missing finding %q in %#v", code, result.Findings)
		}
	}
}

func TestLintRecognizesGoogleDriveServiceAccountAlternative(t *testing.T) {
	workflow := n8n.Workflow{
		Name: "Drive SA",
		Nodes: []n8n.Node{
			{
				ID:   n8n.ID("node-1"),
				Name: "Drive",
				Type: "n8n-nodes-base.googleDrive",
				Parameters: map[string]any{
					"authentication": "serviceAccount",
				},
				Credentials: map[string]n8n.CredentialReference{
					"googleDriveOAuth2Api": {ID: n8n.ID("cred-sa"), Name: "Google SA"},
				},
			},
		},
	}
	credentials := []n8n.Credential{
		{ID: n8n.ID("cred-sa"), Name: "Google SA", Type: "googleApi", ProjectID: n8n.ID("proj-1")},
	}

	result := Lint(workflow, credentials, LintOptions{ProjectID: "proj-1"})
	if result.HasErrors() {
		t.Fatalf("result.HasErrors() = true; findings = %#v", result.Findings)
	}
	if got := statusForNode(result, "Drive"); got != "valid_alternative_type" {
		t.Fatalf("status = %q, want valid_alternative_type; references = %#v", got, result.References)
	}
}

func TestLintClassifiesUnverifiedUnknownNodeType(t *testing.T) {
	workflow := n8n.Workflow{
		Name: "Unknown",
		Nodes: []n8n.Node{
			{
				ID:   n8n.ID("node-1"),
				Name: "Custom",
				Type: "n8n-nodes-custom.thing",
				Credentials: map[string]n8n.CredentialReference{
					"customApi": {ID: n8n.ID("cred-1"), Name: "Custom"},
				},
			},
		},
	}
	credentials := []n8n.Credential{{ID: n8n.ID("cred-1"), Name: "Custom", Type: "otherApi"}}

	result := Lint(workflow, credentials, LintOptions{})
	if result.HasErrors() {
		t.Fatalf("result.HasErrors() = true; findings = %#v", result.Findings)
	}
	if got := statusForNode(result, "Custom"); got != "unable_to_verify" {
		t.Fatalf("status = %q, want unable_to_verify; references = %#v", got, result.References)
	}
}

func TestLintHTTPGenericCredentialType(t *testing.T) {
	workflow := n8n.Workflow{
		Name: "HTTP",
		Nodes: []n8n.Node{
			{
				ID:   n8n.ID("node-1"),
				Name: "HTTP Request",
				Type: "n8n-nodes-base.httpRequest",
				Parameters: map[string]any{
					"authentication":  "genericCredentialType",
					"genericAuthType": "oAuth2Api",
				},
				Credentials: map[string]n8n.CredentialReference{
					"oAuth2Api": {ID: n8n.ID("cred-1"), Name: "OAuth"},
				},
			},
		},
	}
	credentials := []n8n.Credential{{ID: n8n.ID("cred-1"), Name: "OAuth", Type: "oAuth2Api"}}

	result := Lint(workflow, credentials, LintOptions{})
	if result.HasErrors() {
		t.Fatalf("result.HasErrors() = true; findings = %#v", result.Findings)
	}
	if got := statusForNode(result, "HTTP Request"); got != "ok" {
		t.Fatalf("status = %q, want ok; references = %#v", got, result.References)
	}
}

func statusForNode(result Result, nodeName string) string {
	for _, ref := range result.References {
		if ref.NodeName == nodeName {
			return ref.Status
		}
	}
	return ""
}
