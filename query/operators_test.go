package query_test

import (
	"reflect"
	"strings"
	"testing"

	"example.com/huandu-v3/query"
	sqlbuilder "github.com/huandu/go-sqlbuilder"
)

func TestNewScalarOperators(t *testing.T) {
	for _, tc := range []struct {
		name  string
		typ   query.ValueType
		op    query.Operator
		value any
		pg    string
		mysql string
		args  []any
	}{
		{"string_eq", query.String, query.EQ, "A%_!", `LOWER("x") = LOWER($1)`, "LOWER(`x`) = LOWER(?)", []any{"A%_!"}},
		{"string_ne", query.String, query.NE, "A", `LOWER("x") <> LOWER($1)`, "LOWER(`x`) <> LOWER(?)", []any{"A"}},
		{"enum_eq", query.Enum, query.EQ, "HIGH", `LOWER(CAST("x" AS TEXT)) = LOWER($1)`, "LOWER(`x`) = LOWER(?)", []any{"HIGH"}},
		{"enum_in", query.Enum, query.In, []any{"HIGH", "low"}, `LOWER(CAST("x" AS TEXT)) IN (LOWER($1), LOWER($2))`, "LOWER(`x`) IN (LOWER(?), LOWER(?))", []any{"HIGH", "low"}},
		{"enum_not_in", query.Enum, query.NotIn, []any{"low"}, `LOWER(CAST("x" AS TEXT)) NOT IN (LOWER($1))`, "LOWER(`x`) NOT IN (LOWER(?))", []any{"low"}},
		{"counter", query.Counter, query.RangeOp, query.Range{From: int64(10)}, `"x" >= $1`, "`x` >= ?", []any{int64(10)}},
		{"nrange", query.Integer, query.NotRange, query.Range{From: int64(1), To: int64(5)}, `"x" NOT BETWEEN $1 AND $2`, "`x` NOT BETWEEN ? AND ?", []any{int64(1), int64(5)}},
		{"nrange_from", query.Number, query.NotRange, query.Range{From: 1.0}, `"x" < $1`, "`x` < ?", []any{float64(1)}},
		{"nrange_to", query.Counter, query.NotRange, query.Range{To: int64(5)}, `"x" > $1`, "`x` > ?", []any{int64(5)}},
		{"empty", query.DateTime, query.Empty, nil, `"x" IS NULL`, "`x` IS NULL", nil},
		{"nempty", query.DateTime, query.NotEmpty, nil, `"x" IS NOT NULL`, "`x` IS NOT NULL", nil},
	} {
		for _, flavor := range []sqlbuilder.Flavor{sqlbuilder.PostgreSQL, sqlbuilder.MySQL} {
			t.Run(tc.name+"/"+flavor.String(), func(t *testing.T) {
				base := flavor.NewSelectBuilder().Select("x").From("items")
				fields := query.Registry{"field": {Column: "x", Type: tc.typ, Operators: []query.Operator{tc.op}}}
				filtered, err := query.WithFilters(base, fields, query.Filters{"field": {{Op: tc.op, Value: tc.value}}})
				if err != nil {
					t.Fatal(err)
				}
				predicate := tc.pg
				if flavor == sqlbuilder.MySQL {
					predicate = tc.mysql
				}
				got, args := filtered.Build()
				if got != "SELECT x FROM items WHERE "+predicate || !reflect.DeepEqual(args, tc.args) {
					t.Fatalf("%s %#v; want %s %#v", got, args, predicate, tc.args)
				}
			})
		}
	}
}

func TestArraySQLAndParameterOrder(t *testing.T) {
	base := sqlbuilder.PostgreSQL.NewSelectBuilder().Select("a.id").From("assets AS a")
	base.Where(base.Equal("a.tenant_id", 42))
	fields := query.Registry{
		"ports": {Column: "a.ports", Type: query.IntegerArray, Operators: []query.Operator{query.In}},
		"tags":  {Column: "a.tags", Type: query.StringArray, Operators: []query.Operator{query.Contains, query.NotIn}},
	}
	filtered, err := query.WithFilters(base, fields, query.Filters{
		"ports": {{Op: query.In, Value: []any{int64(22), int64(443)}}},
		"tags":  {{Op: query.Contains, Value: "100%_!"}, {Op: query.NotIn, Value: []any{"SERVER"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, args := filtered.Build()
	want := `SELECT a.id FROM assets AS a WHERE a.tenant_id = $1 AND EXISTS (SELECT 1 FROM unnest("a"."ports") AS "__v3_element"("value") WHERE "__v3_element"."value" IN ($2, $3)) AND (EXISTS (SELECT 1 FROM unnest("a"."tags") AS "__v3_element"("value") WHERE "__v3_element"."value" ILIKE $4 ESCAPE '!') OR NOT EXISTS (SELECT 1 FROM unnest("a"."tags") AS "__v3_element"("value") WHERE LOWER("__v3_element"."value") IN (LOWER($5))))`
	if got != want || !reflect.DeepEqual(args, []any{42, int64(22), int64(443), "%100!%!_!!%", "SERVER"}) {
		t.Fatalf("%s %#v\nwant %s", got, args, want)
	}
}

func TestInvalidNewOperations(t *testing.T) {
	for _, tc := range []struct {
		name  string
		typ   query.ValueType
		op    query.Operator
		value any
	}{
		{"contains_list", query.String, query.Contains, []any{"a"}},
		{"contains_number", query.Integer, query.Contains, int64(1)},
		{"contains_enum", query.Enum, query.Contains, "a"},
		{"empty_value", query.DateTime, query.Empty, true},
		{"nempty_value", query.DateTime, query.NotEmpty, "ignored"},
		{"empty_string", query.String, query.Empty, nil},
		{"empty_array", query.StringArray, query.Empty, nil},
		{"nrange_empty", query.Number, query.NotRange, query.Range{}},
		{"nrange_string", query.String, query.NotRange, query.Range{From: "a"}},
		{"array_eq", query.StringArray, query.EQ, "a"},
		{"array_bad_element", query.IntegerArray, query.In, []any{"one"}},
		{"array_empty_input", query.UUIDArray, query.In, []any{}},
		{"array_null_input", query.StringArray, query.NotIn, []any{nil}},
		{"array_typed_list", query.StringArray, query.In, []string{"a"}},
		{"array_substring_list", query.StringArray, query.Contains, []any{"a"}},
		{"array_uuid_contains", query.UUIDArray, query.Contains, "a"},
		{"counter_bad_type", query.Counter, query.RangeOp, query.Range{From: "1.5"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := sqlbuilder.PostgreSQL.NewSelectBuilder().Select("x").From("items")
			before, _ := base.Build()
			fields := query.Registry{"field": {Column: "x", Type: tc.typ, Operators: []query.Operator{tc.op}}}
			filtered, err := query.WithFilters(base, fields, query.Filters{"field": {{Op: tc.op, Value: tc.value}}})
			if filtered != nil || err == nil {
				t.Fatal("invalid filter accepted")
			}
			if after, _ := base.Build(); before != after {
				t.Fatal("base changed on error")
			}
		})
	}
	for _, typ := range []query.ValueType{query.StringArray, query.EnumArray, query.IntegerArray, query.UUIDArray} {
		base := sqlbuilder.MySQL.NewSelectBuilder().Select("x").From("items")
		fields := query.Registry{"field": {Column: "x", Type: typ, Operators: []query.Operator{query.In}}}
		_, err := query.WithFilters(base, fields, query.Filters{"field": {{Op: query.In, Value: []any{"a"}}}})
		if err == nil || !strings.Contains(err.Error(), "require PostgreSQL") {
			t.Fatalf("array filter must reject MySQL: %v", err)
		}
	}
}
