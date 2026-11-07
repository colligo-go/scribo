package reader

import (
	"context"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/exitcode"
	"github.com/colligo-go/scribo/internal/history"
	"github.com/colligo-go/scribo/internal/run"
	"github.com/colligo-go/scribo/internal/validate"
)

type LogRequest struct {
	DB          string
	Table       string
	Key         db.Key
	Field       string
	At          string
	BusyTimeout int
}

type LogResult struct {
	Rows        []db.HistoryRow
	State       db.Row
	Approximate []string
}

func Log(ctx context.Context, backend db.Backend, req LogRequest, warn func(exitcode.Warning)) (LogResult, error) {
	result, err := log(ctx, backend, req, warn)
	return result, run.Interrupted(ctx, err)
}

func log(ctx context.Context, backend db.Backend, req LogRequest, warn func(exitcode.Warning)) (LogResult, error) {
	if err := backend.Open(ctx, req.DB, db.OpenOptions{BusyTimeout: req.BusyTimeout}); err != nil {
		return LogResult{}, err
	}
	defer backend.Close()
	for _, warning := range backend.Warnings() {
		warn(warning)
	}

	names := make([]string, 0, len(req.Key)+1)
	for name := range req.Key {
		names = append(names, name)
	}
	if req.Field != "" {
		names = append(names, req.Field)
	}
	columns, err := validate.Columns(ctx, backend, req.Table, names)
	if err != nil {
		return LogResult{}, err
	}
	uniqueKeys, err := backend.UniqueKeys(ctx, req.Table)
	if err != nil {
		return LogResult{}, err
	}
	if err := validate.Key(req.Table, uniqueKeys, req.Key); err != nil {
		return LogResult{}, err
	}
	key, err := validate.CanonicalKey(columns, req.Key)
	if err != nil {
		return LogResult{}, err
	}
	recordKey, err := history.RecordKey(key)
	if err != nil {
		return LogResult{}, err
	}

	rows, err := backend.HistoryOf(ctx, req.Table, recordKey, req.Field)
	if err != nil {
		return LogResult{}, err
	}
	if len(rows) == 0 {
		exists, err := backend.RecordExists(ctx, req.Table, key)
		if err != nil {
			return LogResult{}, err
		}
		if !exists {
			return LogResult{}, exitcode.New(exitcode.Data, exitcode.CodeKeyNotFound,
				"no record of "+req.Table+" has this key", "")
		}
	}

	result := LogResult{Rows: rows}
	if req.At != "" {
		result.State, result.Approximate = history.StateAt(rows, req.At)
	}
	return result, nil
}
