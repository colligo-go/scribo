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
