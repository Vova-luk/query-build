package query_test

import (
	"database/sql"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"example.com/huandu-v3/query"
	sqlbuilder "github.com/huandu/go-sqlbuilder"
)

func TestPreparedValuesArePassedUnchanged(t *testing.T) {
	day := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	local := time.Date(2026, 10, 21, 12, 30, 0, 0, time.FixedZone("MSK", 3*60*60))
	for _, tc := range []struct {
		name  string
		typ   query.ValueType
		value any
		want  any
	}{
		{"string", query.String, "25", "25"},
		{"integer", query.Integer, int64(25), int64(25)},
		{"integer_precision", query.Integer, int64(9007199254740993), int64(9007199254740993)},
		{"integer_max", query.Integer, int64(math.MaxInt64), int64(math.MaxInt64)},
		{"float64", query.Number, 1.25, float64(1.25)},
		{"boolean", query.Boolean, true, true},
		{"date_time", query.Date, day, day},
		{"datetime_timezone", query.DateTime, local, local},
		{"bytes", query.Bytes, []byte{0, 1, 2}, []byte{0, 1, 2}},
	} {
		for _, flavor := range []sqlbuilder.Flavor{sqlbuilder.PostgreSQL, sqlbuilder.MySQL} {
			t.Run(tc.name+"/"+flavor.String(), func(t *testing.T) {
				base := flavor.NewSelectBuilder().Select("x").From("items")
				fields := query.Registry{"field": {Column: "x", Type: tc.typ, Operators: []query.Operator{query.EQ}}}
				filtered, err := query.WithFilters(base, fields, query.Filters{"field": {{Op: query.EQ, Value: tc.value}}})
				if err != nil {
					t.Fatal(err)
				}
				_, args := filtered.Build()
				if !reflect.DeepEqual(args, []any{tc.want}) {
					t.Fatalf("got %#v, want %#v", args, []any{tc.want})
				}
			})
		}
	}
}

func TestTypeErrorsBeforeQueryIsReturned(t *testing.T) {
	integer := int64(42)
	var missing *int64
	for _, tc := range []struct {
		name  string
		typ   query.ValueType
		op    query.Operator
		value any
		part  string
	}{
		{"required_type", "", query.EQ, nil, "missing or unsupported value type"},
		{"unknown_type", "unsupported", query.EQ, "hello", "unsupported value type"},
		{"integer_text", query.Integer, query.EQ, "25", "expected integer"},
		{"integer_int", query.Integer, query.EQ, 25, "expected integer"},
		{"integer_int32", query.Integer, query.EQ, int32(25), "expected integer"},
		{"integer_uint", query.Integer, query.EQ, uint(25), "expected integer"},
		{"integer_float", query.Integer, query.EQ, float64(25), "expected integer"},
		{"integer_bool", query.Integer, query.EQ, true, "expected integer"},
		{"integer_overflow", query.Integer, query.EQ, uint64(math.MaxUint64), "expected integer"},
		{"json_integer_overflow", query.Integer, query.EQ, json.Number("9223372036854775808"), "expected integer"},
		{"json_integer_fraction", query.Integer, query.EQ, json.Number("1.5"), "expected integer"},
		{"number_text", query.Number, query.EQ, "1.5", "expected number"},
		{"number_integer", query.Number, query.EQ, int64(25), "expected number"},
		{"number_float32", query.Number, query.EQ, float32(1.5), "expected number"},
		{"number_json", query.Number, query.EQ, json.Number("1.5"), "expected number"},
		{"number_nan", query.Number, query.EQ, math.NaN(), "expected number"},
		{"number_inf", query.Number, query.EQ, math.Inf(1), "expected number"},
		{"json_number_overflow", query.Number, query.EQ, json.Number("1e9999"), "expected number"},
		{"json_invalid_number", query.Number, query.EQ, json.Number("hello"), "expected number"},
		{"string_integer", query.String, query.EQ, 25, "expected string"},
		{"string_json_number", query.String, query.EQ, json.Number("25"), "expected string"},
		{"string_bytes", query.String, query.EQ, []byte("hi"), "expected string"},
		{"boolean_string", query.Boolean, query.EQ, "true", "expected boolean"},
		{"boolean_integer", query.Boolean, query.EQ, 1, "expected boolean"},
		{"date_string", query.Date, query.EQ, "2026-10-21", "expected date"},
		{"date_number", query.Date, query.EQ, 1, "expected date"},
		{"datetime_string", query.DateTime, query.EQ, "2026-10-21T12:30:00+03:00", "expected datetime"},
		{"bytes_string", query.Bytes, query.EQ, "hello", "expected bytes"},
		{"valuer_mismatch", query.Integer, query.EQ, sql.NullString{String: "25", Valid: true}, "expected integer"},
		{"valuer_matching", query.String, query.EQ, sql.NullString{String: "hello", Valid: true}, "expected string"},
		{"valuer_null", query.String, query.EQ, sql.NullString{}, "expected string"},
		{"pointer", query.Integer, query.EQ, &integer, "expected integer"},
		{"typed_nil_eq", query.Integer, query.EQ, missing, "expected integer"},
		{"typed_nil_ne", query.Integer, query.NE, missing, "expected integer"},
		{"bytes_nil_eq", query.Bytes, query.EQ, []byte(nil), "expected bytes"},
		{"like_integer", query.Integer, query.Like, "%25%", "not supported for type"},
		{"ilike_date", query.Date, query.ILike, "%2026%", "not supported for type"},
		{"bool_comparison", query.Boolean, query.GTE, true, "not supported for type"},
		{"bytes_comparison", query.Bytes, query.LT, []byte{1}, "not supported for type"},
		{"bool_between", query.Boolean, query.Between, query.Range{From: false, To: true}, "not supported for type"},
		{"string_range", query.String, query.RangeOp, query.Range{From: "a", To: "z"}, "not supported for type"},
		{"mixed_in", query.Integer, query.In, []any{int64(1), "2"}, "value[1]: expected integer"},
		{"in_unprepared_int", query.Integer, query.In, []any{int64(1), 2}, "value[1]: expected integer"},
		{"mixed_not_in", query.String, query.NotIn, []any{"a", 2}, "value[1]: expected string"},
		{"range_from", query.Integer, query.Between, query.Range{From: "1", To: 5}, "from: expected integer"},
		{"range_to", query.Number, query.Between, query.Range{From: 1.0, To: "5"}, "to: expected number"},
		{"range_unprepared_int", query.Integer, query.Between, query.Range{From: int64(1), To: 5}, "to: expected integer"},
		{"open_range", query.Date, query.RangeOp, query.Range{To: "yesterday"}, "to: expected date"},
		{"null_bytes_list", query.Bytes, query.In, []any{[]byte(nil)}, "value[0]: expected bytes"},
	} {
		for _, flavor := range []sqlbuilder.Flavor{sqlbuilder.PostgreSQL, sqlbuilder.MySQL} {
			t.Run(tc.name+"/"+flavor.String(), func(t *testing.T) {
				base := flavor.NewSelectBuilder().Select("x").From("items")
				base.Where(base.Equal("tenant_id", 42))
				before, beforeArgs := base.Build()
				fields := query.Registry{
					"a_valid": {Column: "y", Type: query.Integer, Operators: []query.Operator{query.EQ}},
					"field":   {Column: "x", Type: tc.typ, Operators: []query.Operator{tc.op}},
				}
				filters := query.Filters{
					"a_valid": {{Op: query.EQ, Value: int64(1)}}, // Fail after partially processing a clone.
					"field":   {{Op: tc.op, Value: tc.value}},
				}
				filtered, err := query.WithFilters(base, fields, filters)
				if filtered != nil || err == nil || !strings.Contains(err.Error(), tc.part) || !strings.Contains(err.Error(), `field "field"`) {
					t.Fatalf("got %v, expected field context and %q", err, tc.part)
				}
				after, afterArgs := base.Build()
				if before != after || !reflect.DeepEqual(beforeArgs, afterArgs) {
					t.Fatal("base changed on validation error")
				}
			})
		}
	}
}

func TestTypedListsAndRange(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	for _, flavor := range []sqlbuilder.Flavor{sqlbuilder.PostgreSQL, sqlbuilder.MySQL} {
		base := flavor.NewSelectBuilder().Select("*").From("items")
		fields := query.Registry{
			"ids":  {Column: "id", Type: query.Integer, Operators: []query.Operator{query.In}},
			"when": {Column: "created_at", Type: query.Date, Operators: []query.Operator{query.Between}},
		}
		filtered, err := query.WithFilters(base, fields, query.Filters{
			"ids":  {{Op: query.In, Value: []any{int64(9007199254740993), int64(2)}}},
			"when": {{Op: query.Between, Value: query.Range{From: from, To: to}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, args := filtered.Build()
		want := []any{int64(9007199254740993), int64(2), from, to}
		if !reflect.DeepEqual(args, want) {
			t.Fatalf("got %#v, want %#v", args, want)
		}
	}
}

func TestTypedNulls(t *testing.T) {
	for _, flavor := range []sqlbuilder.Flavor{sqlbuilder.PostgreSQL, sqlbuilder.MySQL} {
		for _, typ := range []query.ValueType{query.String, query.Integer, query.Number, query.Boolean, query.Date, query.DateTime, query.Bytes} {
			for _, op := range []query.Operator{query.EQ, query.NE} {
				base := flavor.NewSelectBuilder().Select("*").From("items")
				fields := query.Registry{"field": {Column: "x", Type: typ, Operators: []query.Operator{op}}}
				filtered, err := query.WithFilters(base, fields, query.Filters{"field": {{Op: op, Value: nil}}})
				if err != nil {
					t.Fatal(err)
				}
				got, args := filtered.Build()
				expr := " IS NULL"
				if op == query.NE {
					expr = " IS NOT NULL"
				}
				if got != "SELECT * FROM items WHERE "+flavor.Quote("x")+expr || len(args) != 0 {
					t.Fatalf("%s/%s: %s %#v", flavor, typ, got, args)
				}
			}
		}
	}
}
