package sqlite

import (
	"errors"

	"github.com/colligo-go/scribo/internal/exitcode"
	"modernc.org/sqlite"
)

const (
	resultPerm       = 3
	resultBusy       = 5
	resultLocked     = 6
	resultReadOnly   = 8
	resultCantOpen   = 14
	resultConstraint = 19
)

func translate(err error) error {
	if err == nil {
		return nil
	}
	var domain *exitcode.Error
	if errors.As(err, &domain) {
		return domain
	}
	var driverErr *sqlite.Error
	if !errors.As(err, &driverErr) {
		return err
	}
	switch driverErr.Code() & 0xff {
	case resultBusy, resultLocked:
		return exitcode.New(exitcode.System, exitcode.CodeDBBusy,
			driverErr.Error(),
			"another process holds the write lock; raise --busy-timeout or spool (SPEC §13)")
	case resultConstraint:
		return exitcode.New(exitcode.Data, exitcode.CodeConstraintFailed, driverErr.Error(), "")
	case resultReadOnly, resultPerm, resultCantOpen:
		return exitcode.New(exitcode.System, exitcode.CodePermissionDenied, driverErr.Error(), "")
	default:
		return exitcode.New(exitcode.System, exitcode.CodeIOError, driverErr.Error(), "")
	}
}
