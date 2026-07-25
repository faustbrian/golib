package apiquerypgx_test

import (
	"context"
	"os"
	"testing"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
	"github.com/faustbrian/golib/pkg/api-query/apiquerypgx"
	"github.com/jackc/pgx/v5"
)

func TestPostgresInjectionResistanceAndStableCursorOrder(t *testing.T) {
	databaseURL := os.Getenv("APIQUERY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("APIQUERY_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close(ctx) })
	_, err = connection.Exec(ctx, `CREATE TEMP TABLE orders (
		id text PRIMARY KEY,
		tenant_id text NOT NULL,
		status text NOT NULL,
		created_at timestamptz NOT NULL
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = connection.Exec(ctx, `INSERT INTO orders (id, tenant_id, status, created_at) VALUES
		('a', 'tenant-42', 'paid', '2026-01-02T00:00:00Z'),
		('b', 'tenant-42', 'paid', '2026-01-02T00:00:00Z'),
		('c', 'tenant-42', 'paid', '2026-01-02T00:00:00Z'),
		('d', 'tenant-42', 'paid', '2026-01-01T00:00:00Z'),
		('foreign', 'tenant-other', 'paid', '2026-01-03T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}

	compiler, err := apiquerypgx.NewCompiler(apiquerypgx.Mapping{
		Fields: map[string]string{"id": "orders.id", "status": "orders.status",
			"created_at": "orders.created_at"},
		Filters:     map[string]string{"status": "orders.status"},
		Sorts:       map[string]string{"created_at": "orders.created_at", "id": "orders.id"},
		Constraints: map[string]string{"tenant_id": "orders.tenant_id"},
	})
	if err != nil {
		t.Fatal(err)
	}

	injectionPlan := postgresPlan(t, "paid' OR true --")
	parts, err := compiler.Compile(injectionPlan)
	if err != nil {
		t.Fatal(err)
	}
	query := "SELECT " + parts.Projection + " FROM orders WHERE " + parts.Where +
		" ORDER BY " + parts.OrderBy
	rows, err := connection.Query(ctx, query, stringArguments(parts.Arguments)...)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		rows.Close()
		t.Fatal("injected filter broadened the query")
	}
	rows.Close()
	var count int
	if scanErr := connection.QueryRow(ctx, "SELECT count(*) FROM orders").Scan(&count); scanErr != nil || count != 5 {
		t.Fatalf("orders table after injection attempt: count=%d error=%v", count, scanErr)
	}

	pagePlan := postgresPlan(t, "paid")
	parts, err = compiler.Compile(pagePlan)
	if err != nil {
		t.Fatal(err)
	}
	query = "SELECT " + parts.Projection + " FROM orders WHERE " + parts.Where +
		" ORDER BY " + parts.OrderBy + " LIMIT 2"
	first := queryIDs(t, ctx, connection, query, stringArguments(parts.Arguments)...)
	if len(first) != 2 || first[0] != "a" || first[1] != "b" {
		t.Fatalf("first page = %v", first)
	}
	_, err = connection.Exec(ctx, `INSERT INTO orders (id, tenant_id, status, created_at)
		VALUES ('aa', 'tenant-42', 'paid', '2026-01-03T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	seekQuery := "SELECT " + parts.Projection + " FROM orders WHERE " + parts.Where +
		" AND (orders.created_at < $3 OR (orders.created_at = $3 AND orders.id > $4))" +
		" ORDER BY " + parts.OrderBy + " LIMIT 2"
	arguments := append(stringArguments(parts.Arguments), "2026-01-02T00:00:00Z", first[1])
	second := queryIDs(t, ctx, connection, seekQuery, arguments...)
	if len(second) != 2 || second[0] != "c" || second[1] != "d" {
		t.Fatalf("second page = %v", second)
	}
}

func postgresPlan(t testing.TB, status string) *apiquery.Plan {
	t.Helper()
	schema, err := apiquery.NewSchema(apiquery.SchemaConfig{
		Resource: "orders", Revision: "v1",
		Fields: []apiquery.FieldDefinition{
			{Name: "id", Type: apiquery.TypeString, Required: true},
			{Name: "status", Type: apiquery.TypeString, Default: true},
			{Name: "created_at", Type: apiquery.TypeTime, Required: true},
		},
		Filters: []apiquery.FilterDefinition{{Name: "status", Type: apiquery.TypeString,
			Operators: []apiquery.Operator{apiquery.OpEqual}, AllowEmpty: true}},
		Sorts: []apiquery.SortDefinition{
			{Name: "created_at", Type: apiquery.TypeTime},
			{Name: "id", Type: apiquery.TypeString, TieBreaker: true},
		},
		DefaultSort: []apiquery.SortTerm{
			{Name: "created_at", Direction: apiquery.Descending},
			{Name: "id", Direction: apiquery.Ascending},
		},
		Pagination: apiquery.PaginationDefinition{Cursor: true, DefaultPageSize: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := apiquery.Compile(context.Background(), schema, apiquery.Request{
		Filter: &apiquery.FilterExpr{Predicate: &apiquery.Predicate{Name: "status",
			Operator: apiquery.OpEqual, Values: []apiquery.Value{apiquery.StringValue(status)}}},
		Page: apiquery.PageRequest{Mode: apiquery.PageCursor, Size: 2},
	}, apiquery.CompileOptions{MandatoryConstraints: []apiquery.Constraint{{
		Name: "tenant_id", Value: apiquery.StringValue("tenant-42"), Protected: true,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func stringArguments(values []apiquery.Value) []any {
	arguments := make([]any, len(values))
	for index := range values {
		arguments[index] = values[index].String()
	}
	return arguments
}

func queryIDs(t testing.TB, ctx context.Context, connection *pgx.Conn, query string, arguments ...any) []string {
	t.Helper()
	rows, err := connection.Query(ctx, query, arguments...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			t.Fatal(err)
		}
		id, ok := values[0].(string)
		if !ok {
			t.Fatalf("id column = %T, want string", values[0])
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}
