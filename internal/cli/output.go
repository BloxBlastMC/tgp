package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/itchyny/gojq"
	"github.com/spf13/cobra"
)

// output describes how a command prints results, from the global flags.
type output struct {
	json   string // "": human output; "*": all fields; else comma-separated fields
	jq     string
	pretty bool // indent JSON (stdout is a terminal)
}

func (o *output) jsonMode() bool { return o.json != "" || o.jq != "" }

// print writes v as JSON (honouring --json fields and --jq) or calls human.
func (a *app) print(cmd *cobra.Command, v any, human func(w io.Writer) error) error {
	if !a.out.jsonMode() {
		return human(a.io.Out)
	}
	return a.printJSON(cmd, v)
}

func (a *app) printJSON(cmd *cobra.Command, v any) error {
	data, err := toGeneric(v)
	if err != nil {
		return err
	}
	if f := a.out.json; f != "" && f != "*" {
		data, err = selectFields(data, splitList(f), outputFields(cmd))
		if err != nil {
			return err
		}
	}
	if a.out.jq != "" {
		return runJQ(a.io.Out, a.out.jq, data, a.out.pretty)
	}
	return writeJSON(a.io.Out, data, a.out.pretty)
}

func toGeneric(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func writeJSON(w io.Writer, v any, pretty bool) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if pretty {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(v)
}

// selectFields keeps only the named top-level fields of an object, or of
// each object in an array. allowed (if known) validates the names.
func selectFields(data any, fields, allowed []string) (any, error) {
	if len(allowed) > 0 {
		ok := map[string]bool{}
		for _, f := range allowed {
			ok[f] = true
		}
		var bad []string
		for _, f := range fields {
			if !ok[f] {
				bad = append(bad, f)
			}
		}
		if len(bad) > 0 {
			return nil, &Error{Code: CodeUsage, Exit: ExitUsage,
				Message: fmt.Sprintf("unknown JSON field(s): %s", strings.Join(bad, ", ")),
				Hint:    "available fields: " + strings.Join(allowed, ", ")}
		}
	}
	pick := func(m map[string]any) map[string]any {
		out := make(map[string]any, len(fields))
		for _, f := range fields {
			if v, ok := m[f]; ok {
				out[f] = v
			} else {
				out[f] = nil
			}
		}
		return out
	}
	switch d := data.(type) {
	case map[string]any:
		return pick(d), nil
	case []any:
		out := make([]any, len(d))
		for i, e := range d {
			if m, ok := e.(map[string]any); ok {
				out[i] = pick(m)
			} else {
				out[i] = e
			}
		}
		return out, nil
	}
	return data, nil
}

func normalizeForJQ(v any) any {
	switch t := v.(type) {
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return int(i)
		}
		f, _ := t.Float64()
		return f
	case map[string]any:
		for k, e := range t {
			t[k] = normalizeForJQ(e)
		}
	case []any:
		for i, e := range t {
			t[i] = normalizeForJQ(e)
		}
	}
	return v
}

// runJQ prints each result of the jq expression: strings raw, everything
// else as JSON, one per line (like `gh --jq`).
func runJQ(w io.Writer, expr string, data any, pretty bool) error {
	q, err := gojq.Parse(expr)
	if err != nil {
		return usageErr("invalid --jq expression: %v", err)
	}
	code, err := gojq.Compile(q)
	if err != nil {
		return usageErr("invalid --jq expression: %v", err)
	}
	iter := code.Run(normalizeForJQ(data))
	for {
		v, ok := iter.Next()
		if !ok {
			return nil
		}
		if err, ok := v.(error); ok {
			if h, ok := err.(*gojq.HaltError); ok && h.Value() == nil {
				return nil
			}
			return errorf("jq_error", ExitError, "--jq: %v", err)
		}
		if s, ok := v.(string); ok {
			fmt.Fprintln(w, s)
			continue
		}
		if err := writeJSON(w, v, pretty); err != nil {
			return err
		}
	}
}

// fieldListRe matches a comma-separated list of JSON field names.
var fieldListRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*(,[a-z_][a-z0-9_]*)*$`)

// rewriteJSONArgs turns `--json a,b` into `--json=a,b` when every name is
// a known output field of the command, so the gh-style spelling works even
// though --json may also be given without a value.
func rewriteJSONArgs(args []string, fields []string) []string {
	if len(fields) == 0 {
		return args
	}
	known := map[string]bool{}
	for _, f := range fields {
		known[f] = true
	}
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			out = append(out, args[i:]...)
			break
		}
		if args[i] == "--json" && i+1 < len(args) && fieldListRe.MatchString(args[i+1]) {
			all := true
			for _, f := range strings.Split(args[i+1], ",") {
				all = all && known[f]
			}
			if all {
				out = append(out, "--json="+args[i+1])
				i++
				continue
			}
		}
		out = append(out, args[i])
	}
	return out
}

// Human output helpers.

func printTable(w io.Writer, s *IOStreams, headers []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if len(headers) > 0 {
		h := make([]string, len(headers))
		for i, x := range headers {
			h[i] = s.Dim(strings.ToUpper(x))
		}
		fmt.Fprintln(tw, strings.Join(h, "\t"))
	}
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	return tw.Flush()
}

// printFields prints aligned "key: value" lines.
func printFields(w io.Writer, s *IOStreams, kv [][2]string) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, p := range kv {
		fmt.Fprintf(tw, "%s\t%s\n", s.Dim(p[0]+":"), p[1])
	}
	return tw.Flush()
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// ago formats a time relative to now ("3 days ago").
func ago(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "never"
	}
	d := time.Since(*t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute") + " ago"
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour") + " ago"
	case d < 30*24*time.Hour:
		return plural(int(d.Hours()/24), "day") + " ago"
	case d < 365*24*time.Hour:
		return plural(int(d.Hours()/24/30), "month") + " ago"
	}
	return plural(int(d.Hours()/24/365), "year") + " ago"
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
