package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/spf13/cobra"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
)

// annOutput is the cobra annotation naming a command's JSON output schema:
// a component schema name ("Repo"), optionally prefixed with "[]" for an
// array. Names resolve against the API spec's components plus cliSchemas.
const annOutput = "tgp.output"

func withOutput(cmd *cobra.Command, schema string) *cobra.Command {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[annOutput] = schema
	return cmd
}

// cliSchemas are output shapes that exist only in the CLI. They may refer to
// API schemas with "#/components/schemas/X".
const cliSchemas = `{
  "AuthStatus": {
    "type": "object",
    "required": ["host", "url", "logged_in", "token_source"],
    "properties": {
      "host": {"type": "string", "description": "Host key, e.g. git.example.com"},
      "url": {"type": "string"},
      "logged_in": {"type": "boolean", "description": "The stored token works."},
      "default": {"type": "boolean", "description": "Used when no --host is given."},
      "user": {"type": "string"},
      "role": {"$ref": "#/components/schemas/Role"},
      "token_source": {"type": "string", "enum": ["keyring", "file", "env", "none"]},
      "token_scopes": {"type": "array", "items": {"type": "string"}},
      "error": {
        "type": "object",
        "required": ["code", "message"],
        "properties": {"code": {"type": "string"}, "message": {"type": "string"}}
      }
    }
  },
  "PullView": {
    "description": "A pull request with all its reviews and comments.",
    "allOf": [
      {"$ref": "#/components/schemas/PullRequest"},
      {
        "type": "object",
        "required": ["reviews", "comment_list"],
        "properties": {
          "reviews": {"type": "array", "items": {"$ref": "#/components/schemas/Review"}},
          "comment_list": {
            "type": "array",
            "description": "Every comment: conversation, inline (path, line, side, diff_hunk, outdated, resolved) and replies (reply_to), oldest first.",
            "items": {"$ref": "#/components/schemas/Comment"}
          }
        }
      }
    ]
  },
  "IssueView": {
    "description": "An issue with all its comments.",
    "allOf": [
      {"$ref": "#/components/schemas/Issue"},
      {
        "type": "object",
        "required": ["comment_list"],
        "properties": {"comment_list": {"type": "array", "items": {"$ref": "#/components/schemas/Comment"}}}
      }
    ]
  },
  "IssueStatus": {
    "type": "object",
    "required": ["assigned", "created"],
    "properties": {
      "assigned": {"type": "array", "items": {"$ref": "#/components/schemas/IssueSummary"}},
      "created": {"type": "array", "items": {"$ref": "#/components/schemas/IssueSummary"}}
    }
  },
  "PullStatus": {
    "type": "object",
    "required": ["review_requested", "authored"],
    "properties": {
      "review_requested": {"type": "array", "items": {"$ref": "#/components/schemas/PullSummary"}},
      "authored": {"type": "array", "items": {"$ref": "#/components/schemas/PullSummary"}}
    }
  },
  "PullCheckout": {
    "type": "object",
    "required": ["number", "branch", "head_sha"],
    "properties": {"number": {"type": "integer"}, "branch": {"type": "string"}, "head_sha": {"type": "string"}}
  },
  "Deleted": {
    "type": "object",
    "required": ["name", "deleted"],
    "properties": {"name": {"type": "string"}, "deleted": {"type": "boolean"}}
  },
  "Logout": {
    "type": "object",
    "required": ["host", "token_revoked"],
    "properties": {
      "host": {"type": "string"},
      "token_revoked": {"type": "boolean", "description": "The token tgp auth login created was revoked on the server."}
    }
  },
  "GitSetup": {
    "type": "object",
    "required": ["urls", "helper"],
    "properties": {
      "urls": {"type": "array", "items": {"type": "string"}, "description": "Server URLs git now gets credentials for from tgp."},
      "helper": {"type": "string", "description": "The credential.helper value written to the global git config."}
    }
  },
  "Clone": {
    "type": "object",
    "required": ["repo", "directory", "clone_url"],
    "properties": {"repo": {"type": "string"}, "directory": {"type": "string"}, "clone_url": {"type": "string"}}
  },
  "SchemaIndexEntry": {
    "type": "object",
    "required": ["command", "schema"],
    "properties": {"command": {"type": "string"}, "schema": {"type": "string"}}
  },
  "Version": {
    "type": "object",
    "required": ["version", "commit", "date", "go_version"],
    "properties": {
      "version": {"type": "string"}, "commit": {"type": "string"},
      "date": {"type": "string"}, "go_version": {"type": "string"}
    }
  }
}`

var (
	specOnce sync.Once
	specDoc  *openapi3.T
	specErr  error
)

// spec returns the API spec with cliSchemas merged in and refs resolved.
func spec() (*openapi3.T, error) {
	specOnce.Do(func() {
		doc, err := apigen.GetSwagger()
		if err != nil {
			specErr = err
			return
		}
		var extra map[string]*openapi3.SchemaRef
		if err := json.Unmarshal([]byte(cliSchemas), &extra); err != nil {
			specErr = err
			return
		}
		for name, s := range extra {
			if _, dup := doc.Components.Schemas[name]; dup {
				specErr = fmt.Errorf("CLI schema %q collides with an API schema", name)
				return
			}
			doc.Components.Schemas[name] = s
		}
		if err := openapi3.NewLoader().ResolveRefsIn(doc, nil); err != nil {
			specErr = err
			return
		}
		specDoc = doc
	})
	return specDoc, specErr
}

// outputSchema returns the schema a command's JSON output conforms to.
func outputSchema(cmd *cobra.Command) (name string, s *openapi3.Schema, ok bool) {
	name = cmd.Annotations[annOutput]
	if name == "" {
		return "", nil, false
	}
	doc, err := spec()
	if err != nil {
		return name, nil, false
	}
	ref := doc.Components.Schemas[strings.TrimPrefix(name, "[]")]
	if ref == nil || ref.Value == nil {
		return name, nil, false
	}
	if strings.HasPrefix(name, "[]") {
		return name, &openapi3.Schema{Type: &openapi3.Types{"array"}, Items: ref}, true
	}
	return name, ref.Value, true
}

// outputFields lists the top-level JSON fields of a command's output.
func outputFields(cmd *cobra.Command) []string {
	_, s, ok := outputSchema(cmd)
	if !ok {
		return nil
	}
	if s.Items != nil && s.Items.Value != nil {
		s = s.Items.Value
	}
	props := map[string]bool{}
	var collect func(*openapi3.Schema)
	collect = func(s *openapi3.Schema) {
		for k := range s.Properties {
			props[k] = true
		}
		for _, sub := range s.AllOf {
			if sub.Value != nil {
				collect(sub.Value)
			}
		}
	}
	collect(s)
	return sortedKeys(props)
}

// jsonSchema renders a command's output schema as a standalone JSON Schema
// (draft 2020-12) document with its dependencies under $defs.
func jsonSchema(cmd *cobra.Command) (map[string]any, error) {
	name := cmd.Annotations[annOutput]
	doc, err := spec()
	if err != nil {
		return nil, err
	}
	base := strings.TrimPrefix(name, "[]")
	if doc.Components.Schemas[base] == nil {
		return nil, fmt.Errorf("unknown schema %q", base)
	}
	out := map[string]any{
		"$schema":     "https://json-schema.org/draft/2020-12/schema",
		"title":       cmd.CommandPath(),
		"description": fmt.Sprintf("JSON output of `%s --json`", cmd.CommandPath()),
	}
	if strings.HasPrefix(name, "[]") {
		out["type"] = "array"
		out["items"] = map[string]any{"$ref": "#/$defs/" + base}
	} else {
		out["$ref"] = "#/$defs/" + base
	}
	defs := map[string]any{}
	queue := []string{base}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if _, done := defs[n]; done {
			continue
		}
		b, err := json.Marshal(doc.Components.Schemas[n].Value)
		if err != nil {
			return nil, err
		}
		var m any
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, err
		}
		defs[n] = toJSONSchema(m, func(ref string) { queue = append(queue, ref) })
	}
	out["$defs"] = defs
	return out, nil
}

// toJSONSchema converts OpenAPI 3.0 schema JSON to JSON Schema: refs point
// at $defs, `nullable` becomes a "null" type and `example` becomes `examples`.
func toJSONSchema(v any, onRef func(string)) any {
	switch t := v.(type) {
	case map[string]any:
		if ref, ok := t["$ref"].(string); ok {
			name := strings.TrimPrefix(ref, "#/components/schemas/")
			onRef(name)
			return map[string]any{"$ref": "#/$defs/" + name}
		}
		out := map[string]any{}
		for k, e := range t {
			switch k {
			case "nullable":
				continue
			case "example":
				out["examples"] = []any{e}
				continue
			case "properties":
				props := map[string]any{}
				for pk, pv := range e.(map[string]any) {
					props[pk] = toJSONSchema(pv, onRef)
				}
				out[k] = props
				continue
			}
			out[k] = toJSONSchema(e, onRef)
		}
		if t["nullable"] == true {
			switch typ := out["type"].(type) {
			case string:
				out["type"] = []any{typ, "null"}
			case []any:
				out["type"] = append(typ, "null")
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = toJSONSchema(e, onRef)
		}
		return out
	}
	return v
}

// commandsWithOutput walks the command tree for commands with a schema.
func commandsWithOutput(root *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Annotations[annOutput] != "" {
			out = append(out, c)
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
	sort.Slice(out, func(i, j int) bool { return out[i].CommandPath() < out[j].CommandPath() })
	return out
}

func commandName(c *cobra.Command) string {
	return strings.TrimPrefix(c.CommandPath(), c.Root().Name()+" ")
}

func newSchemaCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schema [command...]",
		Short: "Show the JSON output schema of a command",
		Long: `Print the JSON Schema (draft 2020-12) describing what a command prints
with --json. Without arguments, list every command that has JSON output and
the name of its schema.`,
		Example: `  tgp schema repo list
  tgp schema --json            # machine-readable index`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := cmd.Root()
			if len(args) == 0 {
				var index []map[string]string
				for _, c := range commandsWithOutput(root) {
					index = append(index, map[string]string{"command": commandName(c), "schema": c.Annotations[annOutput]})
				}
				return a.print(cmd, index, func(w io.Writer) error {
					rows := make([][]string, len(index))
					for i, e := range index {
						rows[i] = []string{e["command"], e["schema"]}
					}
					return printTable(w, a.io, []string{"command", "schema"}, rows)
				})
			}
			target, rest, err := root.Find(args)
			if err != nil || len(rest) > 0 || target == root {
				return usageErr("unknown command %q", strings.Join(args, " "))
			}
			if target.Annotations[annOutput] == "" {
				return usageErr("`tgp %s` has no JSON output", commandName(target))
			}
			s, err := jsonSchema(target)
			if err != nil {
				return err
			}
			// The schema itself is the output: --jq applies, field selection doesn't.
			if a.out.jq != "" {
				g, _ := toGeneric(s)
				return runJQ(a.io.Out, a.out.jq, g, a.out.pretty)
			}
			return writeJSON(a.io.Out, s, true)
		},
	}
	return withOutput(cmd, "[]SchemaIndexEntry")
}
