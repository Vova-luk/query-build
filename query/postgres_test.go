package query_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"example.com/huandu-v3/query"
	sqlbuilder "github.com/huandu/go-sqlbuilder"
)

// Optional integration test. Uses session-local fixtures only, via the psql CLI;
// the package itself has no database-driver dependency.
func TestPostgreSQLFilterSemantics(t *testing.T) {
	dsn := os.Getenv("V3_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set V3_TEST_POSTGRES_DSN and install psql to run PostgreSQL integration tests")
	}
	fields := query.Registry{
		"name":       {Column: "a.name", Type: query.String, Operators: []query.Operator{query.EQ, query.NE, query.Contains, query.NotContains}},
		"importance": {Column: "a.importance", Type: query.Enum, Operators: []query.Operator{query.EQ, query.NE, query.In, query.NotIn}},
		"levels":     {Column: "a.levels", Type: query.EnumArray, Operators: []query.Operator{query.In, query.NotIn}},
		"tags":       {Column: "a.tags", Type: query.StringArray, Operators: []query.Operator{query.In, query.NotIn, query.Contains, query.NotContains}},
		"ports":      {Column: "a.ports", Type: query.IntegerArray, Operators: []query.Operator{query.In, query.NotIn}},
		"owners":     {Column: "a.owners", Type: query.UUIDArray, Operators: []query.Operator{query.In, query.NotIn}},
		"count":      {Column: "a.count", Type: query.Counter, Operators: []query.Operator{query.RangeOp, query.NotRange}},
		"seen":       {Column: "a.seen", Type: query.DateTime, Operators: []query.Operator{query.RangeOp, query.NotRange, query.Empty, query.NotEmpty}},
		"enabled":    {Column: "a.enabled", Type: query.Boolean, Operators: []query.Operator{query.EQ}},
	}
	const owner = "00000000-0000-0000-0000-000000000001"
	sep := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	oct := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		field string
		op    query.Operator
		value any
		ids   string
	}{
		{"eq_literal", "name", query.EQ, "ALPHA%_!", "1"},
		{"eq_case", "name", query.EQ, "ALPHA", "5"},
		{"ne_case", "name", query.NE, "ALPHA", "1,2,3"},
		{"enum_eq", "importance", query.EQ, "high", "1,4"},
		{"enum_ne", "importance", query.NE, "HIGH", "2,5"},
		{"enum_in", "importance", query.In, []any{"high"}, "1,4"},
		{"enum_not_in", "importance", query.NotIn, []any{"LOW"}, "1,4"},
		{"contains_case", "name", query.Contains, "ALPHA", "1,3,5"},
		{"contains_literal", "name", query.Contains, "%_!", "1"},
		{"not_contains_literal", "name", query.NotContains, "%_!", "2,3,5"},
		{"range_both", "count", query.RangeOp, query.Range{From: int64(10), To: int64(20)}, "1,2"},
		{"range_from", "count", query.RangeOp, query.Range{From: int64(10)}, "1,2,4"},
		{"range_to", "count", query.RangeOp, query.Range{To: int64(9)}, "3,5"},
		{"nrange_both", "count", query.NotRange, query.Range{From: int64(10), To: int64(20)}, "3,4,5"},
		{"nrange_from", "count", query.NotRange, query.Range{From: int64(10)}, "3,5"},
		{"nrange_to", "count", query.NotRange, query.Range{To: int64(20)}, "4"},
		{"empty", "seen", query.Empty, nil, "3,5"},
		{"nempty", "seen", query.NotEmpty, nil, "1,2,4"},
		{"datetime_range", "seen", query.RangeOp, query.Range{From: sep}, "1,2"},
		{"datetime_nrange", "seen", query.NotRange, query.Range{From: sep, To: oct}, "4"},
		{"array_string_in", "tags", query.In, []any{"WORKSTATION", "missing"}, "1"},
		{"array_string_not_in", "tags", query.NotIn, []any{"workstation"}, "2,3,4,5"},
		{"array_contains", "tags", query.Contains, "STATION", "1"},
		{"array_not_contains", "tags", query.NotContains, "station", "2,3,4,5"},
		{"array_literal", "tags", query.Contains, "%_!", "1"},
		{"array_enum_in", "levels", query.In, []any{"high"}, "1,2"},
		{"array_enum_not_in", "levels", query.NotIn, []any{"HIGH"}, "3,4,5"},
		{"array_uuid_in", "owners", query.In, []any{owner}, "1"},
		{"array_uuid_not_in", "owners", query.NotIn, []any{owner}, "2,3,4,5"},
		{"array_integer_in", "ports", query.In, []any{int64(443), int64(8080)}, "1"},
		{"array_integer_not_in", "ports", query.NotIn, []any{int64(443)}, "2,3,4,5"},
		{"bool_eq", "enabled", query.EQ, true, "1,3,5"},
		{"injection", "name", query.EQ, "' OR TRUE --", ""},
	}
	var script, want strings.Builder
	script.WriteString(postgresFixture)
	for i, tc := range cases {
		base := sqlbuilder.PostgreSQL.NewSelectBuilder().
			Select("'" + tc.name + ":' || COALESCE(string_agg(a.id::text, ',' ORDER BY a.id), '')").From("v3_items AS a")
		base.Where(base.Equal("a.tenant_id", int64(42)))
		filters := query.Filters{tc.field: {{Op: tc.op, Value: tc.value}}}
		filtered, err := query.WithFilters(base, fields, filters)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		sql, args := filtered.Build()
		fmt.Fprintf(&script, "PREPARE v3_q%d AS %s;\nEXECUTE v3_q%d (", i, sql, i)
		for j, arg := range args {
			if j > 0 {
				script.WriteString(", ")
			}
			script.WriteString(postgresTestLiteral(t, arg))
		}
		script.WriteString(");\n")
		fmt.Fprintf(&want, "%s:%s\n", tc.name, tc.ids)
	}

	// Explicitly exercise OR within one field AND another field AND tenant policy.
	base := sqlbuilder.PostgreSQL.NewSelectBuilder().
		Select("'grouping:' || COALESCE(string_agg(a.id::text, ',' ORDER BY a.id), '')").From("v3_items AS a")
	base.Where(base.Equal("a.tenant_id", int64(42)))
	filtered, err := query.WithFilters(base, fields, query.Filters{
		"name":  {{Op: query.EQ, Value: "BETA"}, {Op: query.EQ, Value: "ALPHA"}},
		"count": {{Op: query.RangeOp, Value: query.Range{From: int64(10)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sql, args := filtered.Build()
	fmt.Fprintf(&script, "PREPARE v3_groups AS %s;\nEXECUTE v3_groups (", sql)
	for j, arg := range args {
		if j > 0 {
			script.WriteString(", ")
		}
		script.WriteString(postgresTestLiteral(t, arg))
	}
	script.WriteString(");\n")
	want.WriteString("grouping:2\n")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "--dbname", dsn)
	cmd.Stdin = strings.NewReader(script.String())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	got, err := cmd.Output()
	if err != nil {
		t.Fatalf("PostgreSQL integration failed: %v\n%s", err, stderr.String())
	}
	if string(got) != want.String() {
		t.Fatalf("PostgreSQL results:\n%s\nwant:\n%s", got, want.String())
	}
}

func postgresTestLiteral(t *testing.T, value any) string {
	t.Helper()
	switch v := value.(type) {
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case time.Time:
		return "'" + v.Format(time.RFC3339Nano) + "'"
	case string:
		return "'" + strings.ReplaceAll(v, "'", "''") + "'"
	default:
		t.Fatalf("unexpected test parameter %T", value)
		return ""
	}
}

const postgresFixture = `
SET standard_conforming_strings = on;
CREATE TEMP TABLE v3_session_marker (id integer);
CREATE TYPE pg_temp.v3_importance AS ENUM ('HIGH', 'LOW');
CREATE TEMP TABLE v3_items (
    id integer, tenant_id integer, name text, importance pg_temp.v3_importance,
    levels pg_temp.v3_importance[], tags text[], ports integer[], owners uuid[],
    seen timestamptz, count bigint, enabled boolean
);
INSERT INTO v3_items VALUES
    (1, 42, 'Alpha%_!', 'HIGH', '{HIGH}', '{WorkStation,"100%_!"}', '{22,443}', '{00000000-0000-0000-0000-000000000001}', '2026-09-01T00:00:00Z', 10, true),
    (2, 42, 'beta', 'LOW', '{LOW,HIGH}', '{server,NULL}', '{80}', '{00000000-0000-0000-0000-000000000002}', '2026-10-01T00:00:00Z', 20, false),
    (3, 42, 'AlphaZz', NULL, '{}', '{}', '{}', '{}', NULL, 0, true),
    (4, 42, NULL, 'HIGH', NULL, NULL, NULL, NULL, '2026-08-31T00:00:00Z', 30, false),
    (5, 42, 'alpha', 'LOW', '{NULL}', '{NULL}', '{NULL}', '{NULL}', NULL, 9, true),
    (99, 99, 'Alpha%_!', 'HIGH', '{HIGH}', '{WorkStation,"100%_!"}', '{22,443}', '{00000000-0000-0000-0000-000000000001}', '2026-09-01T00:00:00Z', 10, true);
`
