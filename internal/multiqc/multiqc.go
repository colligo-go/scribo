package multiqc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/colligo-go/scribo/internal/exitcode"
)

const (
	TypeReal = "REAL"
	TypeText = "TEXT"
)

type Column struct {
	Name string
	Type string
}

type Table struct {
	Columns    []Column
	Rows       map[string]map[string]string
	Versions   map[string]string
	HasVersion bool
	Skipped    []string
}

func (t Table) Samples() []string {
	samples := make([]string, 0, len(t.Rows))
	for sample := range t.Rows {
		samples = append(samples, sample)
	}
	sort.Strings(samples)
	return samples
}

type Report struct {
	data     map[string]map[string]map[string]any
	headers  map[string]map[string]any
	raw      map[string]any
	groups   []string
	moduleOf map[string]string
}

func Read(r io.Reader) (*Report, error) {
	decoder := json.NewDecoder(r)
	decoder.UseNumber()
	var top map[string]any
	if err := decoder.Decode(&top); err != nil {
		return nil, invalidJSON(decoder, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, invalidFormat(fmt.Sprintf("unexpected data after the JSON object at byte %d", decoder.InputOffset()))
	}

	report := &Report{moduleOf: map[string]string{}}
	data, ok := top["report_general_stats_data"].(map[string]any)
	if !ok {
		return nil, invalidFormat("the file has no report_general_stats_data object")
	}
	report.data = make(map[string]map[string]map[string]any, len(data))
	for group, value := range data {
		samples, ok := value.(map[string]any)
		if !ok {
			return nil, invalidFormat(fmt.Sprintf("report_general_stats_data.%s is not an object of samples", group))
		}
		report.data[group] = make(map[string]map[string]any, len(samples))
		for sample, fields := range samples {
			values, ok := fields.(map[string]any)
			if !ok {
				return nil, invalidFormat(fmt.Sprintf("report_general_stats_data.%s.%s is not an object of metrics", group, sample))
			}
			report.data[group][sample] = values
		}
		report.groups = append(report.groups, group)
	}
	sort.Strings(report.groups)

	headers, _ := top["report_general_stats_headers"].(map[string]any)
	report.headers = map[string]map[string]any{}
	for group, value := range headers {
		if metrics, ok := value.(map[string]any); ok {
			report.headers[group] = metrics
		}
	}
	report.raw, _ = top["report_saved_raw_data"].(map[string]any)

	for _, group := range report.groups {
		report.moduleOf[group] = moduleName(group, report.headers[group])
	}
	return report, nil
}

func (r *Report) Modules() []string {
	seen := map[string]bool{}
	var modules []string
	for _, group := range r.groups {
		module := r.moduleOf[group]
		if !seen[module] {
			seen[module] = true
			modules = append(modules, module)
		}
	}
	sort.Strings(modules)
	return modules
}

func (r *Report) GeneralStats() (Table, error) {
	builder := newTableBuilder()
	for _, group := range r.groups {
		module := r.moduleOf[group]
		for _, metric := range sortedKeys(r.headers[group]) {
			for sample, fields := range r.data[group] {
				value, present := fields[metric]
				if !present {
					continue
				}
				if err := builder.add(group, sample, module+"."+metric, metric, value); err != nil {
					return Table{}, err
				}
			}
		}
	}
	return builder.table(), nil
}

func (r *Report) Module(name string) (Table, error) {
	module, found := r.findModule(name)
	if !found {
		return Table{}, exitcode.New(exitcode.File, exitcode.CodeModuleNotFound,
			fmt.Sprintf("the report has no module %s", name),
			"modules: "+strings.Join(r.Modules(), ", "))
	}

	builder := newTableBuilder()
	versionField := module + "_version"
	versions := map[string]string{}
	versionGroup := map[string]string{}
	for _, group := range r.groups {
		if r.moduleOf[group] != module {
			continue
		}
		for sample, fields := range r.data[group] {
			builder.touch(sample)
			for field, value := range fields {
				if field == versionField {
					if err := mergeVersion(versions, versionGroup, group, sample, field, value); err != nil {
						return Table{}, err
					}
					continue
				}
				if err := builder.add(group, sample, field, field, value); err != nil {
					return Table{}, err
				}
			}
		}
	}

	table := builder.table()
	rawSamples, _ := r.raw["multiqc_"+module].(map[string]any)
	for _, sample := range table.Samples() {
		if _, ok := versions[sample]; ok {
			continue
		}
		if version, ok := findVersion(rawSamples[sample], versionField); ok {
			versions[sample] = version
		}
	}
	table.Versions = versions
	table.HasVersion = len(versions) > 0
	return table, nil
}

func (r *Report) findModule(name string) (string, bool) {
	for _, module := range r.Modules() {
		if strings.EqualFold(module, name) {
			return module, true
		}
	}
	return "", false
}

func moduleName(group string, headers map[string]any) string {
	for _, metric := range sortedKeys(headers) {
		header, ok := headers[metric].(map[string]any)
		if !ok {
			continue
		}
		if namespace, ok := header["namespace"].(string); ok && namespace != "" {
			return strings.ToLower(namespace)
		}
	}
	return group
}

func mergeVersion(versions, versionGroup map[string]string, group, sample, field string, value any) error {
	c, ok := cellOf(value)
	if !ok || c.absent {
		return nil
	}
	if previous, seen := versions[sample]; seen && previous != c.text {
		return disagreement(versionGroup[sample], group, field, sample)
	}
	versions[sample] = c.text
	versionGroup[sample] = group
	return nil
}

func findVersion(node any, key string) (string, bool) {
	object, ok := node.(map[string]any)
	if !ok {
		return "", false
	}
	for _, name := range sortedKeys(object) {
		if text, ok := object[name].(string); ok && name == key {
			return text, true
		}
		if version, ok := findVersion(object[name], key); ok {
			return version, true
		}
	}
	return "", false
}

func sortedKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func invalidJSON(decoder *json.Decoder, err error) *exitcode.Error {
	var syntax *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntax):
		return invalidFormat(fmt.Sprintf("invalid JSON at byte %d: %v", syntax.Offset, err))
	case errors.As(err, &typeErr):
		return invalidFormat(fmt.Sprintf("the file is not a JSON object (byte %d)", typeErr.Offset))
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return invalidFormat(fmt.Sprintf("the JSON ends early, at byte %d", decoder.InputOffset()))
	}
	return invalidFormat(fmt.Sprintf("invalid JSON at byte %d: %v", decoder.InputOffset(), err))
}

func invalidFormat(message string) *exitcode.Error {
	return exitcode.New(exitcode.File, exitcode.CodeInvalidFormat, message,
		"give the multiqc_data.json that MultiQC writes in multiqc_data/")
}

func disagreement(first, second, field, sample string) *exitcode.Error {
	return exitcode.New(exitcode.File, exitcode.CodeInvalidFormat,
		fmt.Sprintf("%s and %s disagree on %s for %s", first, second, field, sample),
		"two sections of one MultiQC module give different values for the same metric")
}
