package workflow

import (
	_ "embed"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
)

//go:embed runtime_validator.js
var runtimeValidatorScript string

//go:embed runtime_validator_metadata.json
var runtimeValidatorMetadata string

type runtimeValidationResponse struct {
	Findings []Finding `json:"findings"`
}

func ValidateWithRuntime(path string) ([]Finding, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	cmd := exec.Command("node", "-e", runtimeValidatorScript, absPath)
	cmd.Env = append(os.Environ(), "N8NCTL_VALIDATOR_METADATA="+runtimeValidatorMetadata)
	if validatorDir := findRuntimeValidatorDir(); validatorDir != "" {
		cmd.Env = append(cmd.Env, "N8NCTL_VALIDATOR_NODE_PATH="+validatorDir)
	}
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var response runtimeValidationResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, err
	}
	return response.Findings, nil
}

func findRuntimeValidatorDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(wd, "tools", "n8n-validator")
		if stat, err := os.Stat(filepath.Join(candidate, "package.json")); err == nil && !stat.IsDir() {
			return candidate
		}
		next := filepath.Dir(wd)
		if next == wd {
			return ""
		}
		wd = next
	}
}

func ValidateWorkflowWithRuntime(workflow any) ([]Finding, error) {
	file, err := os.CreateTemp("", "n8nctl-workflow-*.json")
	if err != nil {
		return nil, err
	}
	defer os.Remove(file.Name())

	if err := json.NewEncoder(file).Encode(workflow); err != nil {
		file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return ValidateWithRuntime(file.Name())
}
