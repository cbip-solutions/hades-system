// SPDX-License-Identifier: MIT
// Package format implements the shared CLI output formatter used by every
// doctor). Three formats: table (default, human-readable; uses text/tabwriter),
// json (machine-friendly; encoding/json), yaml (operator paste-into-config;
// gopkg.in/yaml.v3).
//
// Universal flags registered via AttachFlags:
//
// --json bool shortcut for --format=json
// --quiet bool suppress headers + decorative output
// --verbose bool print request URLs + latencies
// --since string time-bound filter (24h, 7d, 2025-01-01)
// --limit int cap rows (default 100)
// --filter string namespace-specific post-fetch filter
// --format string table|json|yaml (default table)
//
// Caller flow:
//
// cmd := &cobra.Command{...}
// format.AttachFlags(cmd)
// cmd.RunE = func(c *cobra.Command, args []string) error {
// if err := format.ValidateExclusive(c); err != nil { return err }
// opts := format.OptionsFromFlags(c)
// rows, err := fetchRows(c, opts) // namespace-specific
// if err != nil { return err }
// return format.Render(c.OutOrStdout(), opts, rows, columns)
// }
package format

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v3"
)

type Options struct {
	Format  string
	Quiet   bool
	Verbose bool
	Since   string
	Limit   int
	Filter  string
}

type Column struct {
	Header string
	Field  func(row any) string
}

func AttachFlags(cmd *cobra.Command) {
	pf := cmd.PersistentFlags()
	if pf.Lookup("json") == nil {
		pf.Bool("json", false, "Equivalent to --format=json")
	}
	if pf.Lookup("quiet") == nil {
		pf.Bool("quiet", false, "Suppress headers and decorative output")
	}
	if pf.Lookup("verbose") == nil {
		pf.Bool("verbose", false, "Print debug info (URLs, latencies)")
	}
	if pf.Lookup("since") == nil {
		pf.String("since", "", "Time-bound filter (24h, 7d, 2025-01-01)")
	}
	if pf.Lookup("limit") == nil {
		pf.Int("limit", 100, "Cap result rows")
	}
	if pf.Lookup("filter") == nil {
		pf.String("filter", "", "Namespace-specific post-fetch filter")
	}
	if pf.Lookup("format") == nil {
		pf.String("format", "table", "Output format: table | json | yaml")
	}
}

func OptionsFromFlags(cmd *cobra.Command) Options {
	jsonFlag := getBoolFlag(cmd, "json")
	quietFlag := getBoolFlag(cmd, "quiet")
	verboseFlag := getBoolFlag(cmd, "verbose")
	sinceFlag := getStringFlag(cmd, "since")
	limitFlag := getIntFlag(cmd, "limit", 100)
	filterFlag := getStringFlag(cmd, "filter")
	formatFlag := getStringFlagDefault(cmd, "format", "table")
	if jsonFlag {
		formatFlag = "json"
	}
	return Options{
		Format:  formatFlag,
		Quiet:   quietFlag,
		Verbose: verboseFlag,
		Since:   sinceFlag,
		Limit:   limitFlag,
		Filter:  filterFlag,
	}
}

func getBoolFlag(cmd *cobra.Command, name string) bool {
	if f := cmd.Flags().Lookup(name); f != nil {
		v, _ := cmd.Flags().GetBool(name)
		return v
	}
	if f := cmd.PersistentFlags().Lookup(name); f != nil {
		v, _ := cmd.PersistentFlags().GetBool(name)
		return v
	}
	return false
}

func getStringFlag(cmd *cobra.Command, name string) string {
	return getStringFlagDefault(cmd, name, "")
}

func getStringFlagDefault(cmd *cobra.Command, name, def string) string {
	if f := cmd.Flags().Lookup(name); f != nil {
		v, _ := cmd.Flags().GetString(name)
		return v
	}
	if f := cmd.PersistentFlags().Lookup(name); f != nil {
		v, _ := cmd.PersistentFlags().GetString(name)
		return v
	}
	return def
}

func getIntFlag(cmd *cobra.Command, name string, def int) int {
	if f := cmd.Flags().Lookup(name); f != nil {
		v, _ := cmd.Flags().GetInt(name)
		return v
	}
	if f := cmd.PersistentFlags().Lookup(name); f != nil {
		v, _ := cmd.PersistentFlags().GetInt(name)
		return v
	}
	return def
}

func ValidateExclusive(cmd *cobra.Command) error {
	q := getBoolFlag(cmd, "quiet")
	v := getBoolFlag(cmd, "verbose")
	if q && v {
		return errors.New("flags --quiet and --verbose are mutually exclusive")
	}
	return nil
}

func ValidateFormat(opts Options) error {
	switch opts.Format {
	case "table", "json", "yaml":
		return nil
	default:
		return fmt.Errorf("unknown format %q (want table|json|yaml)", opts.Format)
	}
}

func ParseSince(s string) (time.Time, error) {
	return ParseSinceAt(s, time.Now())
}

func ParseSinceAt(s string, now time.Time) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}

	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}

	if strings.HasSuffix(s, "d") {
		nStr := strings.TrimSuffix(s, "d")
		if n, err := strconv.Atoi(nStr); err == nil {
			return now.Add(-time.Duration(n) * 24 * time.Hour), nil
		}
	}

	if d, err := time.ParseDuration(s); err == nil {
		return now.Add(-d), nil
	}
	return time.Time{}, fmt.Errorf("invalid --since %q (expected duration like 24h or date YYYY-MM-DD)", s)
}

func ParseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, errors.New("empty duration")
	}
	if strings.HasSuffix(s, "d") {
		nStr := strings.TrimSuffix(s, "d")
		if n, err := strconv.Atoi(nStr); err == nil {
			return time.Duration(n) * 24 * time.Hour, nil
		}
	}
	return time.ParseDuration(s)
}

func Render(w io.Writer, opts Options, rows any, columns []Column) error {
	if err := ValidateFormat(opts); err != nil {
		return err
	}
	rowSlice, err := toSlice(rows)
	if err != nil {
		return err
	}
	if opts.Filter != "" {
		filtered, ferr := ApplyFilter(rowSlice, opts.Filter, columns)
		if ferr != nil {
			return ferr
		}
		rowSlice = filtered

		switch opts.Format {
		case "table":
			return renderTable(w, opts, rowSlice, columns)
		case "json":
			return renderJSON(w, rowSlice)
		case "yaml":
			return renderYAML(w, rowSlice)
		}
	}
	switch opts.Format {
	case "table":
		return renderTable(w, opts, rowSlice, columns)
	case "json":
		return renderJSON(w, rows)
	case "yaml":
		return renderYAML(w, rows)
	default:
		return fmt.Errorf("unknown format %q", opts.Format)
	}
}

func ApplyFilter(rows []any, filter string, columns []Column) ([]any, error) {
	if filter == "" {
		return rows, nil
	}
	clauses, err := parseFilterClauses(filter)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		matched := true
		for _, c := range clauses {
			val, ok := lookupRowValue(r, c.key, columns)
			if !ok {
				matched = false
				break
			}
			if c.op == "=" {
				if !strings.EqualFold(val, c.val) {
					matched = false
					break
				}
			} else {
				re, rerr := regexp.Compile(c.val)
				if rerr != nil {
					return nil, fmt.Errorf("invalid --filter regex %q: %w", c.val, rerr)
				}
				if !re.MatchString(val) {
					matched = false
					break
				}
			}
		}
		if matched {
			out = append(out, r)
		}
	}
	return out, nil
}

type filterClause struct {
	key string
	op  string
	val string
}

func parseFilterClauses(filter string) ([]filterClause, error) {
	parts := strings.Split(filter, ",")
	out := make([]filterClause, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		var op string
		var idx int
		if i := strings.Index(p, "~"); i > 0 {
			op = "~"
			idx = i
		} else if i := strings.Index(p, "="); i > 0 {
			op = "="
			idx = i
		} else {
			return nil, fmt.Errorf("invalid --filter clause %q: expected key=value or key~regex", p)
		}
		key := strings.TrimSpace(p[:idx])
		val := strings.TrimSpace(p[idx+1:])
		if key == "" {
			return nil, fmt.Errorf("invalid --filter clause %q: empty key", p)
		}
		out = append(out, filterClause{key: key, op: op, val: val})
	}
	return out, nil
}

func lookupRowValue(row any, key string, columns []Column) (string, bool) {

	for _, c := range columns {
		if strings.EqualFold(c.Header, key) {
			return c.Field(row), true
		}
	}

	v := reflect.ValueOf(row)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return "", false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return "", false
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if strings.EqualFold(f.Name, key) {
			return reflectFieldString(v.Field(i)), true
		}

		for _, tag := range []string{"json", "yaml"} {
			raw := f.Tag.Get(tag)
			if raw == "" || raw == "-" {
				continue
			}
			tagName := raw
			if c := strings.IndexByte(raw, ','); c >= 0 {
				tagName = raw[:c]
			}
			if strings.EqualFold(tagName, key) {
				return reflectFieldString(v.Field(i)), true
			}
		}
	}
	return "", false
}

func reflectFieldString(v reflect.Value) string {
	if !v.IsValid() {
		return ""
	}
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, 64)
	}
	return fmt.Sprintf("%v", v.Interface())
}

func toSlice(rows any) ([]any, error) {
	v := reflect.ValueOf(rows)
	if v.Kind() != reflect.Slice {
		return nil, fmt.Errorf("rows must be a slice, got %T", rows)
	}
	out := make([]any, v.Len())
	for i := 0; i < v.Len(); i++ {
		out[i] = v.Index(i).Interface()
	}
	return out, nil
}

func renderTable(w io.Writer, opts Options, rows []any, cols []Column) error {
	if len(rows) == 0 {
		_, err := io.WriteString(w, "(no rows)\n")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if !opts.Quiet {

		headers := make([]string, len(cols))
		for i, c := range cols {
			headers[i] = c.Header
		}
		if _, err := fmt.Fprintln(tw, strings.Join(headers, "\t")); err != nil {
			return err
		}
	}
	for _, r := range rows {
		fields := make([]string, len(cols))
		for i, c := range cols {
			fields[i] = c.Field(r)
		}
		if _, err := fmt.Fprintln(tw, strings.Join(fields, "\t")); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func renderJSON(w io.Writer, rows any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

func renderYAML(w io.Writer, rows any) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	defer enc.Close()
	return enc.Encode(rows)
}
