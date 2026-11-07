package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/colligo-go/scribo/internal/exitcode"
	"github.com/colligo-go/scribo/internal/format"
	"github.com/colligo-go/scribo/internal/reader"
	"github.com/colligo-go/scribo/internal/validate"
	"github.com/spf13/cobra"
)

type readFlags struct {
	where     []string
	isNull    []string
	notNull   []string
	fields    []string
	format    string
	forceText bool
}

func (f *readFlags) register(command *cobra.Command, formats string) {
	command.Flags().StringArrayVar(&f.where, "where", nil, "FIELD<op>VALUE with op one of = != > < >= <=; repeatable, joined by AND")
	command.Flags().StringArrayVar(&f.isNull, "is-null", nil, "keep only the records where FIELD is NULL; repeatable")
	command.Flags().StringArrayVar(&f.notNull, "not-null", nil, "keep only the records where FIELD is not NULL; repeatable")
	command.Flags().BoolVar(&f.forceText, "force-text", false, "compare every --where as text, also with > < >= <=")
	command.Flags().StringVar(&f.format, "format", "tsv", "output format: "+formats)
}

func parseRead(options *globalOptions, flags *readFlags, name string, formats []string) (reader.QueryRequest, error) {
	if err := requireGlobals(options, name); err != nil {
		return reader.QueryRequest{}, err
	}
	if !contains(formats, flags.format) {
		return reader.QueryRequest{}, usageFor(name, "--format must be %s, not %q", strings.Join(formats, ", "), flags.format)
	}
	fields, err := parseFields(flags.fields, name)
	if err != nil {
		return reader.QueryRequest{}, err
	}
	for _, field := range append(append([]string(nil), flags.isNull...), flags.notNull...) {
		if field == "" {
			return reader.QueryRequest{}, usageFor(name, "--is-null and --not-null need a field name")
		}
	}
	filters := validate.Filters{Where: flags.where, IsNull: flags.isNull, NotNull: flags.notNull, ForceText: flags.forceText}
	if _, err := filters.Fields(); err != nil {
		return reader.QueryRequest{}, err
	}
	return reader.QueryRequest{
		DB:          options.db,
		Table:       options.table,
		Fields:      fields,
		Filters:     filters,
		BusyTimeout: options.busyTimeout,
	}, nil
}

func parseFields(texts []string, name string) ([]string, error) {
	var fields []string
	seen := map[string]bool{}
	for _, text := range texts {
		for _, field := range strings.Split(text, ",") {
			if field == "" {
				return nil, usageFor(name, "--fields %q has an empty field name", text)
			}
			if seen[field] {
				return nil, usageFor(name, "field %q is given more than once in --fields", field)
			}
			seen[field] = true
			fields = append(fields, field)
		}
	}
	return fields, nil
}

func contains(list []string, text string) bool {
	for _, item := range list {
		if item == text {
			return true
		}
	}
	return false
}

func writeTable(out io.Writer, name string, result reader.QueryResult) error {
	switch name {
	case "json":
		return format.JSON(out, result.Columns, result.Rows)
	case "csv":
		return format.CSV(out, result.Columns, result.Rows)
	case "pretty":
		return format.Pretty(out, result.Columns, result.Rows)
	}
	return format.TSV(out, result.Columns, result.Rows)
}

func refuseExisting(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return exitcode.New(exitcode.File, exitcode.CodeFileExists,
			fmt.Sprintf("%s already exists", path), "pass --overwrite to replace it")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return outputError(err, path)
	}
	return nil
}

func writeFileAtomic(path string, content []byte) error {
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".scribo-*")
	if err != nil {
		return outputError(err, path)
	}
	if err := fillAndRename(temp, path, content, mode); err != nil {
		temp.Close()
		if removeErr := os.Remove(temp.Name()); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			return outputError(removeErr, temp.Name())
		}
		return outputError(err, path)
	}
	return nil
}

func fillAndRename(temp *os.File, path string, content []byte, mode fs.FileMode) error {
	if _, err := io.Copy(temp, bytes.NewReader(content)); err != nil {
		return err
	}
	if err := temp.Chmod(mode); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

func outputError(err error, path string) error {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return exitcode.New(exitcode.System, exitcode.CodePermissionDenied,
			fmt.Sprintf("cannot write %s: permission denied", path), "the file and its directory must be writable")
	case errors.Is(err, fs.ErrNotExist):
		return exitcode.New(exitcode.File, exitcode.CodeFileNotFound,
			fmt.Sprintf("the directory of %s does not exist", path), "")
	}
	return exitcode.New(exitcode.System, exitcode.CodeIOError, fmt.Sprintf("cannot write %s: %v", path, err), "")
}
