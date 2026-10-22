package query_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"example.com/huandu-v3/query"
	sqlbuilder "github.com/huandu/go-sqlbuilder"
)

func TestGroupedFiltersAndNativeQueryOptions(t *testing.T) {
	fields := query.Registry{
		"score":  {Column: "a.score", Type: query.Number, Operators: []query.Operator{query.Between, query.GTE}},
		"status": {Column: "a.status", Type: query.String, Operators: []query.Operator{query.EQ}},
	}
	filters := query.Filters{
		"score":  {{Op: query.Between, Value: query.Range{From: 7.0, To: 9.0}}, {Op: query.GTE, Value: 10.0}},
		"status": {{Op: query.EQ, Value: "active"}, {Op: query.EQ, Value: "pending"}},
	}
	for _, tc := range []struct {
		flavor sqlbuilder.Flavor
		want   string
	}{
		{sqlbuilder.PostgreSQL, `SELECT a.status, COUNT(*) FROM assets AS a LEFT JOIN owners AS o ON a.owner_id = o.id AND o.active = $1 WHERE a.tenant_id = $2 AND ("a"."score" BETWEEN $3 AND $4 OR "a"."score" >= $5) AND (LOWER("a"."status") = LOWER($6) OR LOWER("a"."status") = LOWER($7)) GROUP BY a.status HAVING COUNT(*) > $8 ORDER BY a.status ASC, COUNT(*) DESC LIMIT $9 OFFSET $10`},
		{sqlbuilder.MySQL, "SELECT a.status, COUNT(*) FROM assets AS a LEFT JOIN owners AS o ON a.owner_id = o.id AND o.active = ? WHERE a.tenant_id = ? AND (`a`.`score` BETWEEN ? AND ? OR `a`.`score` >= ?) AND (LOWER(`a`.`status`) = LOWER(?) OR LOWER(`a`.`status`) = LOWER(?)) GROUP BY a.status HAVING COUNT(*) > ? ORDER BY a.status ASC, COUNT(*) DESC LIMIT ? OFFSET ?"},
	} {
		t.Run(tc.flavor.String(), func(t *testing.T) {
			base := tc.flavor.NewSelectBuilder().Select("a.status", "COUNT(*)").From("assets AS a")
			base.JoinWithOption(sqlbuilder.LeftJoin, "owners AS o",
				base.Equal("a.owner_id", sqlbuilder.Raw("o.id")), base.Equal("o.active", true))
			base.Where(base.Equal("a.tenant_id", 42))
			base.GroupBy("a.status").Having(base.GreaterThan("COUNT(*)", 2))
			base.OrderByAsc("a.status").Limit(100).Offset(5)
			before, beforeArgs := base.Build()

			for i := 0; i < 10; i++ { // Map order and template reuse must be stable.
				filtered, err := query.WithFilters(base, fields, filters)
				if err != nil {
					t.Fatal(err)
				}
				got, args := filtered.OrderByDesc("COUNT(*)").Limit(20).Offset(0).Build()
				wantArgs := []any{true, 42, float64(7), float64(9), float64(10), "active", "pending", 2, 20, 0}
				if got != tc.want || !reflect.DeepEqual(args, wantArgs) {
					t.Fatalf("got  %s\n%#v\nwant %s\n%#v", got, args, tc.want, wantArgs)
				}
			}
			after, afterArgs := base.Build()
			if after != before || !reflect.DeepEqual(afterArgs, beforeArgs) {
				t.Fatal("base query was changed")
			}
		})
	}
}

func TestOperators(t *testing.T) {
	date := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name      string
		typ       query.ValueType
		op        query.Operator
		value     any
		predicate string
		args      []any
	}{
		{"eq", query.Integer, query.EQ, int64(5), `"x" = $1`, []any{int64(5)}},
		{"ne", query.Integer, query.NE, int64(5), `"x" <> $1`, []any{int64(5)}},
		{"gt", query.Integer, query.GT, int64(5), `"x" > $1`, []any{int64(5)}},
		{"gte", query.Integer, query.GTE, int64(5), `"x" >= $1`, []any{int64(5)}},
		{"lt", query.Integer, query.LT, int64(5), `"x" < $1`, []any{int64(5)}},
		{"lte", query.Integer, query.LTE, int64(5), `"x" <= $1`, []any{int64(5)}},
		{"null", query.Integer, query.EQ, nil, `"x" IS NULL`, nil},
		{"not_null", query.String, query.NE, nil, `"x" IS NOT NULL`, nil},
		{"in", query.Integer, query.In, []any{int64(1), int64(2)}, `"x" IN ($1, $2)`, []any{int64(1), int64(2)}},
		{"not_in", query.Integer, query.NotIn, []any{int64(1), int64(2)}, `"x" NOT IN ($1, $2)`, []any{int64(1), int64(2)}},
		{"contains", query.String, query.Contains, "a%_!", `"x" ILIKE $1 ESCAPE '!'`, []any{"%a!%!_!!%"}},
		{"not_contains", query.String, query.NotContains, "a%_!", `"x" NOT ILIKE $1 ESCAPE '!'`, []any{"%a!%!_!!%"}},
		{"like", query.String, query.Like, "a%_", `"x" LIKE $1`, []any{"a%_"}},
		{"ilike", query.String, query.ILike, "%a%", `"x" ILIKE $1`, []any{"%a%"}},
		{"between", query.Integer, query.Between, query.Range{From: int64(1), To: int64(5)}, `"x" BETWEEN $1 AND $2`, []any{int64(1), int64(5)}},
		{"range", query.Integer, query.RangeOp, query.Range{From: int64(1), To: int64(5)}, `"x" BETWEEN $1 AND $2`, []any{int64(1), int64(5)}},
		{"from", query.Integer, query.RangeOp, query.Range{From: int64(1)}, `"x" >= $1`, []any{int64(1)}},
		{"to", query.Integer, query.RangeOp, query.Range{To: int64(5)}, `"x" <= $1`, []any{int64(5)}},
		{"date", query.Date, query.GTE, date, `"x" >= $1`, []any{date}},
		{"bool", query.Boolean, query.EQ, true, `"x" = $1`, []any{true}},
	} {
		for _, flavor := range []sqlbuilder.Flavor{sqlbuilder.PostgreSQL, sqlbuilder.MySQL} {
			t.Run(tc.name+"/"+flavor.String(), func(t *testing.T) {
				base := flavor.NewSelectBuilder().Select("x").From("items")
				fields := query.Registry{"field": {Column: "x", Type: tc.typ, Operators: []query.Operator{tc.op}}}
				filtered, err := query.WithFilters(base, fields, query.Filters{"field": {{Op: tc.op, Value: tc.value}}})
				if err != nil {
					t.Fatal(err)
				}
				predicate := tc.predicate
				if flavor == sqlbuilder.MySQL {
					predicate = strings.NewReplacer(`"`, "`", "$1", "?", "$2", "?").Replace(predicate)
					if tc.op == query.ILike {
						predicate = "LOWER(`x`) LIKE LOWER(?)"
					}
					if tc.op == query.Contains {
						predicate = "LOWER(`x`) LIKE LOWER(?) ESCAPE '!'"
					}
					if tc.op == query.NotContains {
						predicate = "LOWER(`x`) NOT LIKE LOWER(?) ESCAPE '!'"
					}
				}
				got, args := filtered.Build()
				if got != "SELECT x FROM items WHERE "+predicate || !reflect.DeepEqual(args, tc.args) {
					t.Fatalf("%s %#v; want predicate %s args %#v", got, args, predicate, tc.args)
				}
			})
		}
	}
}

func TestInvalidInputLeavesBaseUnchanged(t *testing.T) {
	var missing *int
	for _, tc := range []struct {
		name  string
		field string
		col   string
		op    query.Operator
		value any
	}{
		{"unknown_field", "unknown", "x", query.EQ, 1},
		{"unknown_op", "field", "x", "oops", 1},
		{"empty_column", "field", "", query.EQ, 1},
		{"expression", "field", "LOWER(x)", query.EQ, 1},
		{"column_injection", "field", "x; DROP TABLE items", query.EQ, 1},
		{"empty_part", "field", "a..x", query.EQ, 1},
		{"null_comparison", "field", "x", query.GTE, nil},
		{"typed_null", "field", "x", query.GTE, missing},
		{"null_in", "field", "x", query.In, []any{nil}},
		{"empty_in", "field", "x", query.In, []any{}},
		{"scalar_in", "field", "x", query.In, 1},
		{"typed_list", "field", "x", query.In, []int{1, 2}},
		{"list_scalar", "field", "x", query.EQ, []any{1, 2}},
		{"empty_range", "field", "x", query.RangeOp, query.Range{}},
		{"partial_between", "field", "x", query.Between, query.Range{From: 1}},
		{"list_between", "field", "x", query.Between, []any{1, 2}},
		{"null_bound", "field", "x", query.Between, query.Range{From: missing, To: 5}},
		{"bad_to", "field", "x", query.RangeOp, query.Range{To: []any{1}}},
		{"like_number", "field", "x", query.Like, 5},
		{"raw_sql", "field", "x", query.EQ, sqlbuilder.Raw("1 OR TRUE")},
		{"subquery", "field", "x", query.EQ, sqlbuilder.NewSelectBuilder().Select("1")},
		{"map", "field", "x", query.EQ, map[string]any{"sql": "1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := sqlbuilder.PostgreSQL.NewSelectBuilder().Select("x").From("items")
			base.Where(base.Equal("tenant_id", 42))
			before, beforeArgs := base.Build()
			ops := []query.Operator{query.EQ, query.GTE, query.In, query.Between, query.RangeOp, query.Like}
			if tc.name == "unknown_op" {
				ops = append(ops, tc.op) // Even server configuration cannot enable unknown operators.
			}
			typ := query.Integer
			if tc.op == query.Like {
				typ = query.String
			}
			fields := query.Registry{"field": {Column: tc.col, Type: typ, Operators: ops}}
			got, err := query.WithFilters(base, fields, query.Filters{tc.field: {{Op: tc.op, Value: tc.value}}})
			if err == nil || got != nil {
				t.Fatalf("invalid input accepted: %v", err)
			}
			after, afterArgs := base.Build()
			if before != after || !reflect.DeepEqual(beforeArgs, afterArgs) {
				t.Fatal("base changed on error")
			}
		})
	}
}

func TestEmptyAndForbiddenFilters(t *testing.T) {
	if _, err := query.WithFilters(nil, nil, nil); err == nil {
		t.Fatal("nil builder accepted")
	}
	if _, err := query.WithFilters(sqlbuilder.SQLite.NewSelectBuilder(), nil, nil); err == nil {
		t.Fatal("unsupported flavor accepted")
	}
	base := sqlbuilder.PostgreSQL.NewSelectBuilder().Select("x").From("items")
	before, beforeArgs := base.Build()
	filtered, err := query.WithFilters(base, nil, nil)
	if err != nil || filtered == base {
		t.Fatalf("empty filter: %v", err)
	}
	got, args := filtered.Build()
	if got != before || !reflect.DeepEqual(args, beforeArgs) {
		t.Fatal("empty filters changed SQL")
	}
	fields := query.Registry{"x": {Column: "x", Type: query.Integer, Operators: []query.Operator{query.EQ}}}
	if _, err := query.WithFilters(base, fields, query.Filters{"x": {}}); err == nil {
		t.Fatal("empty field accepted")
	}
	if _, err := query.WithFilters(base, fields, query.Filters{"x": {{Op: query.GTE, Value: 1}}}); err == nil {
		t.Fatal("forbidden operator accepted")
	}
}

func TestInjectionIsBoundAsData(t *testing.T) {
	attack := "x' OR 1=1; DROP TABLE items --"
	for _, flavor := range []sqlbuilder.Flavor{sqlbuilder.PostgreSQL, sqlbuilder.MySQL} {
		base := flavor.NewSelectBuilder().Select("x").From("items")
		fields := query.Registry{"name": {Column: "x", Type: query.String, Operators: []query.Operator{query.EQ, query.In, query.Like}}}
		for _, condition := range []query.Condition{
			{Op: query.EQ, Value: attack},
			{Op: query.In, Value: []any{attack}},
			{Op: query.Like, Value: attack},
		} {
			filtered, err := query.WithFilters(base, fields, query.Filters{"name": {condition}})
			if err != nil {
				t.Fatal(err)
			}
			got, args := filtered.Build()
			if strings.Contains(got, attack) || !reflect.DeepEqual(args, []any{attack}) {
				t.Fatalf("value was not bound: %s %#v", got, args)
			}
		}
	}
}
