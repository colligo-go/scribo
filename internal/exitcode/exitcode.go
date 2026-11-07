package exitcode

import "fmt"

const (
	OK       = 0
	Usage    = 1
	Database = 2
	Data     = 3
	File     = 4
	System   = 5
)

const (
	CodeUsage        = "usage"
	CodeInvalidValue = "invalid_value"
	CodeInternal     = "internal"

	CodeFileNotFound       = "file_not_found"
	CodePermissionDenied   = "permission_denied"
	CodePragmaIneffective  = "pragma_ineffective"
	CodeIOError            = "io_error"
	CodeNetworkFilesystem  = "network_filesystem"
	CodeContractMismatch   = "contract_mismatch"
	CodeDBBusy             = "db_busy"
	CodeConstraintFailed   = "constraint_failed"
	CodeReservedTable      = "reserved_table"
	CodeAmbiguousKey       = "ambiguous_key"
	CodeTableNotFound      = "table_not_found"
	CodeColumnNotFound     = "column_not_found"
	CodeKeyNotFound        = "key_not_found"
	CodeKeyAlreadyExists   = "key_already_exists"
	CodeHistoryReconciled  = "history_reconciled"
	CodeHistoryApproximate = "history_approximate"
	CodeKeyMismatch        = "key_mismatch"
	CodeNoUniqueKey        = "no_unique_key"
	CodeInterrupted        = "interrupted"
	CodeUntypedComparison  = "untyped_comparison"
	CodeUnknownSample      = "unknown_sample"
	CodeInputNotFound      = "input_not_found"
	CodeInvalidRules       = "invalid_rules"
	CodeInvariantViolated  = "invariant_violated"
	CodeRuleUnavailable    = "rule_unavailable"
	CodeColligoRequired    = "colligo_required"
	CodeStaleResults       = "stale_results"
	CodeNondeterministic   = "nondeterministic_change"
	CodeInvalidFormat      = "invalid_format"
	CodeBatchFailed        = "batch_failed"
	CodeSyncedPath         = "synced_path"
	CodeRunNotFound        = "run_not_found"
	CodeToolVersionUnknown = "tool_version_unknown"
)

type Error struct {
	Status  int
	Code    string
	Message string
	Context string
}

func New(status int, code, message, context string) *Error {
	return &Error{Status: status, Code: code, Message: message, Context: context}
}

func (e *Error) Error() string {
	return e.Message
}

func (e *Error) Format() string {
	text := fmt.Sprintf("ERROR [%s]: %s\n", e.Code, e.Message)
	if e.Context != "" {
		text += "→ " + e.Context + "\n"
	}
	return text
}

type Warning struct {
	Code    string
	Message string
	Context string
}

func (w Warning) Format() string {
	text := fmt.Sprintf("WARNING [%s]: %s\n", w.Code, w.Message)
	if w.Context != "" {
		text += "→ " + w.Context + "\n"
	}
	return text
}
