package clierrors

import "errors"

const (
	ExitOK         = 0
	ExitInternal   = 1
	ExitUsage      = 2
	ExitSafety     = 3
	ExitResolution = 4
	ExitAPI        = 5

	ExitCIStructuralValidation = 20
	ExitCICredentialPreflight  = 21
	ExitCIDriftFound           = 22
	ExitCIExecutionFailure     = 23
)

const (
	CodeConfigMissing       = "config_missing"
	CodeConfigInvalid       = "config_invalid"
	CodeValidationFailed    = "validation_failed"
	CodeUsageError          = "usage_error"
	CodeSafetyBlocked       = "safety_blocked"
	CodeAPIKeyMissing       = "api_key_missing"
	CodeEnvironmentMissing  = "environment_missing"
	CodeWorkflowNotFound    = "workflow_not_found"
	CodeWorkflowAmbiguous   = "workflow_ambiguous"
	CodeProjectNotFound     = "project_not_found"
	CodeProjectAmbiguous    = "project_ambiguous"
	CodeProjectMismatch     = "project_location_mismatch"
	CodeDriftFound          = "drift_found"
	CodeExecutionFailed     = "execution_failed"
	CodeUnsupportedEndpoint = "unsupported_endpoint"
	CodeAPIFailure          = "api_failure"
	CodeInternalFailure     = "internal_failure"
)

type CLIError struct {
	Code     string
	Message  string
	ExitCode int
	Details  map[string]any
	Err      error
}

func (e *CLIError) Error() string {
	return e.Message
}

func (e *CLIError) Unwrap() error {
	return e.Err
}

func New(exitCode int, code, message string, details map[string]any) *CLIError {
	return &CLIError{
		Code:     code,
		Message:  message,
		ExitCode: exitCode,
		Details:  cloneDetails(details),
	}
}

func Wrap(err error, exitCode int, code, message string, details map[string]any) *CLIError {
	return &CLIError{
		Code:     code,
		Message:  message,
		ExitCode: exitCode,
		Details:  cloneDetails(details),
		Err:      err,
	}
}

func As(err error) *CLIError {
	if err == nil {
		return nil
	}

	var cliErr *CLIError
	if errors.As(err, &cliErr) {
		return cliErr
	}

	return &CLIError{
		Code:     CodeInternalFailure,
		Message:  err.Error(),
		ExitCode: ExitInternal,
	}
}

func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	return As(err).ExitCode
}

func Code(err error) string {
	if err == nil {
		return ""
	}
	return As(err).Code
}

func Details(err error) map[string]any {
	if err == nil {
		return nil
	}
	return cloneDetails(As(err).Details)
}

func cloneDetails(details map[string]any) map[string]any {
	if len(details) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(details))
	for key, value := range details {
		cloned[key] = value
	}
	return cloned
}
