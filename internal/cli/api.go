package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func newAPICmd(a *app) *cobra.Command {
	var (
		method      string
		rawFields   []string
		typedFields []string
		headers     []string
		input       string
		include     bool
	)
	cmd := &cobra.Command{
		Use:   "api [METHOD] PATH",
		Short: "Make an authenticated API request",
		Long: `Send a request to the tgp REST API and print the response.

PATH is relative to /api/v1 (e.g. /repos or repos/game-server); paths that
start with /api/ are used as-is. METHOD defaults to GET, or POST when fields
or --input are given.

Fields build a JSON body (or the query string for GET/DELETE):
  -f key=value   string value
  -F key=value   typed value: numbers, true, false and null are converted;
                 @file reads the value from a file (@- for stdin)
Nested JSON isn't built from fields; use --input with a JSON file instead.

Non-2xx responses exit with the matching exit code (see tgp --help) and
print the API error. The API reference is at /api/v1/openapi.json on the server.`,
		Example: `  tgp api /repos
  tgp api repos/game-server --jq .clone_url
  tgp api PATCH repos/game-server -f description="Paper plugins"
  tgp api POST admin/users -f username=carol -f role=tester -F all_repos=true
  tgp api POST /repos --input repo.json`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[len(args)-1]
			if len(args) == 2 {
				if method != "" {
					return usageErr("give the method either as an argument or with --method, not both")
				}
				method = args[0]
			}
			method = strings.ToUpper(method)
			fields, err := parseFields(a, rawFields, typedFields)
			if err != nil {
				return err
			}
			if method == "" {
				method = http.MethodGet
				if len(fields) > 0 || input != "" {
					method = http.MethodPost
				}
			}
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			if !strings.HasPrefix(path, "/api/") {
				path = "/api/v1" + path
			}
			hdr := http.Header{}
			for _, h := range headers {
				k, v, ok := strings.Cut(h, ":")
				if !ok {
					return usageErr("invalid header %q: use 'Name: value'", h)
				}
				hdr.Add(strings.TrimSpace(k), strings.TrimSpace(v))
			}
			var body []byte
			switch {
			case input != "":
				if len(fields) > 0 {
					return usageErr("--input can't be combined with -f/-F")
				}
				if input == "-" {
					body, err = io.ReadAll(a.io.In)
				} else {
					body, err = os.ReadFile(input)
				}
				if err != nil {
					return err
				}
				if hdr.Get("Content-Type") == "" {
					hdr.Set("Content-Type", "application/json")
				}
			case len(fields) > 0 && (method == http.MethodGet || method == http.MethodDelete || method == http.MethodHead):
				q := url.Values{}
				for _, k := range sortedKeys(fields) {
					q.Set(k, fmt.Sprint(fields[k]))
				}
				sep := "?"
				if strings.Contains(path, "?") {
					sep = "&"
				}
				path += sep + q.Encode()
			case len(fields) > 0:
				if body, err = json.Marshal(fields); err != nil {
					return err
				}
				hdr.Set("Content-Type", "application/json")
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			resp, err := c.raw(cmd.Context(), method, path, hdr, body)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			if include {
				printResponseHead(a.io.Out, resp)
			}
			if resp.StatusCode >= 400 {
				return apiError(resp.StatusCode, data)
			}
			isJSON := strings.Contains(resp.Header.Get("Content-Type"), "json")
			if len(data) == 0 {
				return nil
			}
			if !isJSON {
				if a.out.jq != "" {
					return usageErr("--jq needs a JSON response; got %s", resp.Header.Get("Content-Type"))
				}
				_, err := a.io.Out.Write(data)
				return err
			}
			var v any
			d := json.NewDecoder(bytes.NewReader(data))
			d.UseNumber()
			if err := d.Decode(&v); err != nil {
				return errorf(CodeBadResponse, ExitError, "invalid JSON in response: %v", err)
			}
			if a.out.jq != "" {
				return runJQ(a.io.Out, a.out.jq, v, a.out.pretty)
			}
			if f := a.out.json; f != "" && f != "*" {
				if v, err = selectFields(v, splitList(f), nil); err != nil {
					return err
				}
			}
			return writeJSON(a.io.Out, v, a.out.pretty)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&method, "method", "X", "", "HTTP method")
	f.StringArrayVarP(&rawFields, "raw-field", "f", nil, "add a string field `key=value`")
	f.StringArrayVarP(&typedFields, "field", "F", nil, "add a typed field `key=value` (number, bool, null, @file)")
	f.StringArrayVarP(&headers, "header", "H", nil, "add a request header `'Name: value'`")
	f.StringVar(&input, "input", "", "send the body from a `file` (- for stdin)")
	f.BoolVarP(&include, "include", "i", false, "print the response status line and headers")
	return cmd
}

func parseFields(a *app, raw, typed []string) (map[string]any, error) {
	fields := map[string]any{}
	for _, kv := range raw {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, usageErr("invalid field %q: use key=value", kv)
		}
		fields[k] = v
	}
	for _, kv := range typed {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, usageErr("invalid field %q: use key=value", kv)
		}
		switch {
		case strings.HasPrefix(v, "@"):
			var b []byte
			var err error
			if v == "@-" {
				b, err = io.ReadAll(a.io.In)
			} else {
				b, err = os.ReadFile(v[1:])
			}
			if err != nil {
				return nil, err
			}
			fields[k] = string(b)
		case v == "true":
			fields[k] = true
		case v == "false":
			fields[k] = false
		case v == "null":
			fields[k] = nil
		default:
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				fields[k] = n
			} else if f, err := strconv.ParseFloat(v, 64); err == nil {
				fields[k] = f
			} else {
				fields[k] = v
			}
		}
	}
	return fields, nil
}

func printResponseHead(w io.Writer, resp *http.Response) {
	fmt.Fprintf(w, "%s %s\n", resp.Proto, resp.Status)
	keys := make([]string, 0, len(resp.Header))
	for k := range resp.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.EqualFold(k, "Set-Cookie") {
			continue
		}
		for _, v := range resp.Header[k] {
			fmt.Fprintf(w, "%s: %s\n", k, v)
		}
	}
	fmt.Fprintln(w)
}
