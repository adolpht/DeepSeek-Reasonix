package agent

import "fmt"

// ExitCode represents a semantic exit code for headless execution.
type ExitCode int

const (
	// ExitSuccess indicates the task completed successfully.
	ExitSuccess ExitCode = 0
	// ExitError indicates the task failed due to an agent error.
	ExitError ExitCode = 1
	// ExitPermissionDenied indicates a permission request was denied in non-interactive mode.
	ExitPermissionDenied ExitCode = 2
	// ExitTimeout indicates the execution timed out.
	ExitTimeout ExitCode = 3
	// ExitContextOverflow indicates the context window was exceeded even after compaction.
	ExitContextOverflow ExitCode = 4
	// ExitAPIError indicates a model API error (rate limit, auth, unavailable).
	ExitAPIError ExitCode = 5
)

// SemanticError is an error that carries a semantic exit code for CI/CD integration.
type SemanticError struct {
	Code    ExitCode
	Message string
	Cause   error
}

func (e *SemanticError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *SemanticError) Unwrap() error { return e.Cause }

// NewPermissionError returns a SemanticError with ExitPermissionDenied.
func NewPermissionError(msg string, cause error) *SemanticError {
	return &SemanticError{Code: ExitPermissionDenied, Message: msg, Cause: cause}
}

// NewTimeoutError returns a SemanticError with ExitTimeout.
func NewTimeoutError(msg string, cause error) *SemanticError {
	return &SemanticError{Code: ExitTimeout, Message: msg, Cause: cause}
}

// NewContextOverflowError returns a SemanticError with ExitContextOverflow.
func NewContextOverflowError(msg string) *SemanticError {
	return &SemanticError{Code: ExitContextOverflow, Message: msg}
}

// NewAPIError returns a SemanticError with ExitAPIError.
func NewAPIError(msg string, cause error) *SemanticError {
	return &SemanticError{Code: ExitAPIError, Message: msg, Cause: cause}
}

// ExitCodeFromError returns the semantic exit code for an error.
// If the error is a SemanticError, its Code is returned.
// Otherwise, it returns ExitError (1) for non-nil errors, ExitSuccess (0) for nil.
func ExitCodeFromError(err error) ExitCode {
	if err == nil {
		return ExitSuccess
	}
	if se, ok := err.(*SemanticError); ok {
		return se.Code
	}
	return ExitError
}
