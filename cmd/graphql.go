package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/executor"
	"github.com/spf13/cobra"
	"github.com/tidwall/pretty"
	"github.com/vektah/gqlparser/v2/formatter"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/input"
	"github.com/alphaleonis/nibs/internal/output"
)

var (
	queryJSON       bool
	queryVariables  string
	queryOperation  string
	querySchemaOnly bool
)

var queryCmd = &cobra.Command{
	Use:     "query [query]",
	Aliases: []string{"graphql"},
	Short:   "Run a GraphQL query or mutation",
	Long: `Run a GraphQL query or mutation against the nibs data.

The query is the precision path: fetch exactly the fields you need across many
nibs, traverse relationships in one hop, or batch mutations. Output is the raw
selection ({nib}/{nibs}/…) with no envelope — pass --json to strip colors for
piping.

Pass the query through the escaping-proof input channel so multi-line GraphQL
never has to survive shell quoting:

  # "-" reads the query from stdin
  echo '{ nibs { id title status } }' | nibs query -

  # "@FILE" reads the query from a file
  nibs query @query.graphql --json

A short one-liner may still be passed inline, but "-"/"@FILE" is the primary
form for anything non-trivial:

  nibs query '{ nib(id: "abc") { title status } }'

More examples:
  # Filter nibs by status
  nibs query --json '{ nibs(filter: { status: ["todo", "in-progress"] }) { id title } }'

  # Traverse relationships in one query
  nibs query --json '{ nib(id: "abc") { title parent { title } children { id status } } }'

  # Use variables (inline JSON or "@FILE")
  nibs query -v '{"id": "abc"}' 'query GetNib($id: ID!) { nib(id: $id) { title } }'
  nibs query -v @vars.json @query.graphql

  # Print the schema
  nibs query --schema`,
	Args: func(cmd *cobra.Command, args []string) error {
		if querySchemaOnly {
			return nil
		}
		// Allow 0 args (query comes from piped stdin) or exactly 1 arg.
		// Route the arity violation through the coded envelope (exit 2, and the
		// {error} envelope for --json), uniform with the coded* validators — the
		// --schema bypass above and the 0-or-1-arg stdin logic are why this stays
		// a bespoke validator rather than codedMaximumNArgs.
		if len(args) > 1 {
			return cmdError(queryJSON, output.ErrValidation,
				`%s accepts at most 1 argument (the GraphQL query, or "-"/"@FILE"), got %d`,
				invokedName(cmd), len(args))
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Schema-only mode — no App needed, schema is purely structural
		if querySchemaOnly {
			return printSchema()
		}

		app := getApp(cmd)

		query, err := resolveQuery(args)
		if err != nil {
			// A missing "@FILE"/failed stdin read is FILE_ERROR; a malformed
			// "@" is a usage error — inputError maps each to its exit code.
			return inputError(queryJSON, err)
		}
		if strings.TrimSpace(query) == "" {
			return cmdError(queryJSON, output.ErrValidation,
				`no query provided (pass it inline, use "-"/"@FILE", or pipe to stdin)`)
		}

		variables, err := resolveVariables(queryVariables)
		if err != nil {
			return inputError(queryJSON, err)
		}

		result, err := executeQuery(app, query, variables, queryOperation)
		if err != nil {
			// A GraphQL parse/validation/execution failure — route it through
			// the coded boundary so both modes get a structured, non-zero exit.
			return cmdError(queryJSON, output.ErrValidation, "%s", err)
		}

		// Output (both modes are prettified, but --json skips color)
		if queryJSON {
			fmt.Println(string(pretty.Pretty(result)))
		} else {
			fmt.Println(string(pretty.Color(pretty.Pretty(result), nil)))
		}

		return nil
	},
}

// resolveQuery resolves the GraphQL query text from the positional arg or piped
// stdin. A positional "-" reads stdin and "@FILE" reads the named file — the
// escaping-proof channel, reused from internal/input. Any other positional is
// taken verbatim as a literal inline query (for short one-liners). With no
// positional arg, piped stdin is read automatically if present.
func resolveQuery(args []string) (string, error) {
	if len(args) == 1 {
		arg := args[0]
		if arg == "-" || strings.HasPrefix(arg, "@") {
			return input.Prose(arg, os.Stdin)
		}
		return arg, nil
	}
	return readFromStdin()
}

// resolveVariables parses the --variables value into a map. Inline JSON is the
// common form; an "@FILE" value reads the JSON from a file (reusing
// internal/input) so a large variable set need not ride on a shell argument. A
// missing file surfaces as *input.IOError (FILE_ERROR); malformed JSON is a
// validation error.
func resolveVariables(value string) (map[string]any, error) {
	if value == "" {
		return nil, nil
	}
	raw := value
	if strings.HasPrefix(value, "@") {
		resolved, err := input.Prose(value, os.Stdin)
		if err != nil {
			return nil, err
		}
		raw = resolved
	}
	var variables map[string]any
	if err := json.Unmarshal([]byte(raw), &variables); err != nil {
		return nil, fmt.Errorf("invalid variables JSON: %w", err)
	}
	return variables, nil
}

// readFromStdin reads the query from stdin if data is available.
func readFromStdin() (string, error) {
	// Check if stdin has data (is a pipe or file, not a terminal)
	stat, err := os.Stdin.Stat()
	if err != nil {
		return "", fmt.Errorf("checking stdin: %w", err)
	}

	// If stdin is a terminal (no pipe), return empty
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		return "", nil
	}

	// Read all data from stdin
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("reading stdin: %w", err)
	}

	return strings.TrimSpace(string(data)), nil
}

// executeQuery runs a GraphQL query against the nibs core.
// On success, it returns just the data portion of the response.
// On error, it returns an error so the CLI can handle it appropriately.
func executeQuery(app *App, query string, variables map[string]any, operationName string) ([]byte, error) {
	es := graph.NewExecutableSchema(graph.Config{
		Resolvers: app.newResolver(),
	})

	exec := executor.New(es)

	ctx := graphql.StartOperationTrace(context.Background())
	params := &graphql.RawParams{
		Query:         query,
		Variables:     variables,
		OperationName: operationName,
	}

	opCtx, errs := exec.CreateOperationContext(ctx, params)
	if errs != nil {
		return nil, formatGraphQLErrors(errs)
	}

	ctx = graphql.WithOperationContext(ctx, opCtx)
	handler, ctx := exec.DispatchOperation(ctx, opCtx)
	resp := handler(ctx)

	if len(resp.Errors) > 0 {
		return nil, formatGraphQLErrors(resp.Errors)
	}

	return resp.Data, nil
}

// formatGraphQLErrors formats GraphQL errors into a single error.
//
// Repeated messages are collapsed to their first occurrence. One refused filter
// inside a nested resolver raises its own error per matched parent — a single
// bad children(filter:{parentId:"zz"}) under an unfiltered outer query emits one
// identical sentence per nib in the store — and every copy after the first says
// nothing the first did not, while all of them land in one --json message string
// and in an agent's context. First-encountered order is kept so what survives
// still reads in the order gqlgen reported it.
func formatGraphQLErrors(errs gqlerror.List) error {
	if len(errs) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(errs))
	var msgs []string
	for _, e := range errs {
		if seen[e.Message] {
			continue
		}
		seen[e.Message] = true
		msgs = append(msgs, e.Message)
	}
	if len(msgs) == 1 {
		return fmt.Errorf("graphql: %s", msgs[0])
	}
	return fmt.Errorf("graphql errors:\n  %s", strings.Join(msgs, "\n  "))
}

// printSchema outputs the GraphQL schema.
func printSchema() error {
	fmt.Print(GetGraphQLSchema())
	return nil
}

// GetGraphQLSchema returns the GraphQL schema as a string.
// The schema is purely structural and does not require any runtime state.
func GetGraphQLSchema() string {
	es := graph.NewExecutableSchema(graph.Config{
		Resolvers: &graph.Resolver{},
	})

	var buf bytes.Buffer
	f := formatter.NewFormatter(&buf, formatter.WithIndent("  "))
	f.FormatSchema(es.Schema())

	return buf.String()
}

func init() {
	queryCmd.Flags().BoolVar(&queryJSON, "json", false, "Output JSON without colors (for piping)")
	queryCmd.Flags().StringVarP(&queryVariables, "variables", "v", "", `Query variables as inline JSON or "@FILE"`)
	queryCmd.Flags().StringVarP(&queryOperation, "operation", "o", "", "Operation name (for multi-operation documents)")
	queryCmd.Flags().BoolVar(&querySchemaOnly, "schema", false, "Print the GraphQL schema and exit")
	rootCmd.AddCommand(queryCmd)
}
