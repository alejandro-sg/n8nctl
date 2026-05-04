package credential

import (
	"fmt"
	"strings"

	"github.com/alejandro-sg/n8nctl/pkg/n8n"
)

type credentialRule struct {
	AcceptedTypes []string
	Required      bool
	Known         bool
}

func credentialRuleFor(node n8n.Node, credentialKey string) credentialRule {
	key := strings.TrimSpace(credentialKey)
	switch node.Type {
	case "n8n-nodes-base.googleDrive":
		return selectedGoogleRule(node, key, "googleDriveOAuth2Api")
	case "n8n-nodes-base.googleSheets":
		return selectedGoogleRule(node, key, "googleSheetsOAuth2Api")
	case "n8n-nodes-base.gmail":
		return credentialRule{AcceptedTypes: []string{"gmailOAuth2"}, Required: true, Known: true}
	case "n8n-nodes-base.httpRequest":
		return httpRequestRule(node, key)
	default:
		if key == "" {
			return credentialRule{}
		}
		return credentialRule{AcceptedTypes: []string{key}, Known: strings.Contains(node.Type, "n8n-nodes-base.")}
	}
}

func requiredCredentialRules(node n8n.Node) []credentialRule {
	switch node.Type {
	case "n8n-nodes-base.googleDrive":
		return []credentialRule{selectedGoogleRule(node, "", "googleDriveOAuth2Api")}
	case "n8n-nodes-base.googleSheets":
		return []credentialRule{selectedGoogleRule(node, "", "googleSheetsOAuth2Api")}
	case "n8n-nodes-base.gmail":
		return []credentialRule{{AcceptedTypes: []string{"gmailOAuth2"}, Required: true, Known: true}}
	case "n8n-nodes-base.httpRequest":
		rule := httpRequestRule(node, "")
		if rule.Required {
			return []credentialRule{rule}
		}
	}
	return nil
}

func selectedGoogleRule(node n8n.Node, credentialKey string, oauthType string) credentialRule {
	authentication := strings.TrimSpace(stringValue(node.Parameters["authentication"]))
	switch authentication {
	case "serviceAccount":
		return credentialRule{AcceptedTypes: []string{"googleApi"}, Required: true, Known: true}
	case "oAuth2":
		return credentialRule{AcceptedTypes: []string{oauthType}, Required: true, Known: true}
	case "":
		return credentialRule{AcceptedTypes: []string{oauthType, "googleApi"}, Required: true, Known: true}
	default:
		types := []string{oauthType, "googleApi"}
		if credentialKey != "" && !containsString(types, credentialKey) {
			types = append(types, credentialKey)
		}
		return credentialRule{AcceptedTypes: types, Required: true, Known: true}
	}
}

func httpRequestRule(node n8n.Node, credentialKey string) credentialRule {
	params := node.Parameters
	authentication := strings.TrimSpace(stringValue(params["authentication"]))
	switch authentication {
	case "predefinedCredentialType":
		if selected := strings.TrimSpace(stringValue(params["nodeCredentialType"])); selected != "" {
			return credentialRule{AcceptedTypes: []string{selected}, Required: true, Known: true}
		}
	case "genericCredentialType":
		if selected := strings.TrimSpace(stringValue(params["genericAuthType"])); selected != "" {
			return credentialRule{AcceptedTypes: []string{selected}, Required: true, Known: true}
		}
	}
	if credentialKey != "" {
		return credentialRule{AcceptedTypes: []string{credentialKey}, Required: authentication == "predefinedCredentialType" || authentication == "genericCredentialType", Known: true}
	}
	return credentialRule{Known: true}
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		text := strings.TrimSpace(fmt.Sprintf("%v", value))
		if text == "<nil>" {
			return ""
		}
		return text
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
