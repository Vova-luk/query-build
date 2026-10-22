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

func TestValuesArePreparedByFieldType(t *testing.T) {
	day := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		typ   query.ValueType
		value any
		want  any
	}{
		{"integer_text", query.Integer, "25", int64(25)},
		{"integer_text_min", query.Integer, "-9223372036854775808", int64(math.MinInt64)},
		{"integer_json_precision", query.Integer, json.Number("9007199254740993"), int64(9007199254740993)},
		{"integer_json_max", query.Integer, json.Number("9223372036854775807"), int64(math.MaxInt64)},
		{"counter_text", query.Counter, "10", int64(10)},
		{"number_text", query.Number, "1.5", float64(1.5)},
		{"number_json", query.Number, json.Number("1.5e2"), float64(150)},
		{"boolean_text", query.Boolean, "true", true},
		{"boolean_text_false", query.Boolean, "false", false},
		{"boolean_text_zero", query.Boolean, "0", false},
		{"date_text", query.Date, "2026-10-21", day},
	} {
		for _, flavor := range []sqlbuilder.Flavor{sqlbuilder.PostgreSQL, sqlbuilder.MySQL} {
			t.Run(tc.name+"/"+flavor.String(), func(t *testing.T) {
				base := flavor.NewSelectBuilder().Select("x").From("items")
				fields := query.Registry{"field": {Column: "x", Type: tc.typ, Operators: []query.Operator{query.EQ}}}
				filtered, err := query.WithFilters(base, fields, query.Filters{"field": {{Op: query.EQ, Value: tc.value}}})
				if err != nil {
					t.Fatal(err)
				}
				got, args := filtered.Build()
				if got != "SELECT x FROM items WHERE "+flavor.Quote("x")+" = "+placeholder(flavor) || !reflect.DeepEqual(args, []any{tc.want}) {
					t.Fatalf("%s %#v; want parsed value %#v", got, args, tc.want)
				}
			})
		}
	}
}

func TestNumericValuesKeepTheirGoTypes(t *testing.T) {
	values := []any{
		int(25), int8(-25), int16(25), int32(25), int64(9007199254740993),
		int64(math.MinInt64), int64(math.MaxInt64),
		uint(25), uint8(25), uint16(25), uint32(25), uint64(math.MaxUint64),
		float32(25.5), float64(25.5), float64(1 << 53), -float64(1 << 53),
	}
	for _, flavor := range []sqlbuilder.Flavor{sqlbuilder.PostgreSQL, sqlbuilder.MySQL} {
		for _, typ := range []query.ValueType{query.Integer, query.Counter, query.Number} {
			for _, value := range values {
				base := flavor.NewSelectBuilder().Select("x").From("items")
				fields := query.Registry{"x": {Column: "x", Type: typ, Operators: []query.Operator{query.EQ}}}
				filtered, err := query.WithFilters(base, fields, query.Filters{"x": {{Op: query.EQ, Value: value}}})
				if err != nil {
					t.Fatalf("%s/%s/%T: %v", flavor, typ, value, err)
				}
				_, args := filtered.Build()
				if !reflect.DeepEqual(args, []any{value}) {
					t.Fatalf("%s/%s: numeric type/value changed: got %#v, want %#v", flavor, typ, args, value)
				}
			}
		}
	}
}

func TestParsedDateTimePreservesOffset(t *testing.T) {
	for _, flavor := range []sqlbuilder.Flavor{sqlbuilder.PostgreSQL, sqlbuilder.MySQL} {
		base := flavor.NewSelectBuilder().Select("x").From("items")
		fields := query.Registry{"field": {Column: "x", Type: query.DateTime, Operators: []query.Operator{query.EQ}}}
		filtered, err := query.WithFilters(base, fields, query.Filters{"field": {{Op: query.EQ, Value: "2026-10-21T12:30:00.123456789+03:00"}}})
		if err != nil {
			t.Fatal(err)
		}
		_, args := filtered.Build()
		if len(args) != 1 {
			t.Fatalf("unexpected arguments: %#v", args)
		}
		got, ok := args[0].(time.Time)
		_, offset := got.Zone()
		want := time.Date(2026, 10, 21, 9, 30, 0, 123456789, time.UTC)
		if !ok || !got.Equal(want) || offset != 3*60*60 {
			t.Fatalf("date/time or offset changed: %#v", args[0])
		}
	}
}

func placeholder(flavor sqlbuilder.Flavor) string {
	if flavor == sqlbuilder.PostgreSQL {
		return "$1"
	}
	return "?"
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
		{"integer_text", query.Integer, query.EQ, "hello", "expected integer"},
		{"integer_text_fraction", query.Integer, query.EQ, "25.5", "expected integer"},
		{"integer_text_overflow", query.Integer, query.EQ, "9223372036854775808", "expected integer"},
		{"integer_text_underflow", query.Integer, query.EQ, "-9223372036854775809", "expected integer"},
		{"integer_float_nan", query.Integer, query.EQ, math.NaN(), "expected integer"},
		{"integer_float_inf", query.Integer, query.EQ, math.Inf(1), "expected integer"},
		{"integer_float32_nan", query.Integer, query.EQ, float32(math.NaN()), "expected integer"},
		{"integer_float32_inf", query.Integer, query.EQ, float32(math.Inf(1)), "expected integer"},
		{"integer_bool", query.Integer, query.EQ, true, "expected integer"},
		{"json_integer_overflow", query.Integer, query.EQ, json.Number("9223372036854775808"), "expected integer"},
		{"json_integer_fraction", query.Integer, query.EQ, json.Number("1.5"), "expected integer"},
		{"number_text", query.Number, query.EQ, "hello", "expected number"},
		{"number_bool", query.Number, query.EQ, true, "expected number"},
		{"number_text_nan", query.Number, query.EQ, "NaN", "expected number"},
		{"number_text_inf", query.Number, query.EQ, "+Inf", "expected number"},
		{"number_nan", query.Number, query.EQ, math.NaN(), "expected number"},
		{"number_inf", query.Number, query.EQ, math.Inf(1), "expected number"},
		{"number_float32_nan", query.Number, query.EQ, float32(math.NaN()), "expected number"},
		{"number_float32_inf", query.Number, query.EQ, float32(math.Inf(-1)), "expected number"},
		{"json_number_overflow", query.Number, query.EQ, json.Number("1e9999"), "expected number"},
		{"json_invalid_number", query.Number, query.EQ, json.Number("hello"), "expected number"},
		{"string_integer", query.String, query.EQ, 25, "expected string"},
		{"string_json_number", query.String, query.EQ, json.Number("25"), "expected string"},
		{"string_bytes", query.String, query.EQ, []byte("hi"), "expected string"},
		{"boolean_string", query.Boolean, query.EQ, "yes", "expected boolean"},
		{"boolean_integer", query.Boolean, query.EQ, 1, "expected boolean"},
		{"date_string", query.Date, query.EQ, "2026-02-30", "expected date"},
		{"date_number", query.Date, query.EQ, 1, "expected date"},
		{"datetime_string", query.DateTime, query.EQ, "2026-10-21", "expected datetime"},
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
		{"mixed_in", query.Integer, query.In, []any{"1", "two"}, "value[1]: expected integer"},
		{"scalar_string_in", query.String, query.In, "Alice", "requires a nonempty []any"},
		{"contains_integer", query.String, query.Contains, 25, "expected string"},
		{"integer_contains", query.Integer, query.Contains, "bad", "not supported for type"},
		{"mixed_not_in", query.String, query.NotIn, []any{"a", 2}, "value[1]: expected string"},
		{"range_from", query.Integer, query.Between, query.Range{From: "one", To: 5}, "from: expected integer"},
		{"range_to", query.Number, query.Between, query.Range{From: "1", To: "five"}, "to: expected number"},
		{"range_fraction", query.Integer, query.Between, query.Range{From: 1, To: "5.5"}, "to: expected integer"},
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

func TestListsAndDateTimeRangeArePrepared(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	for _, flavor := range []sqlbuilder.Flavor{sqlbuilder.PostgreSQL, sqlbuilder.MySQL} {
		base := flavor.NewSelectBuilder().Select("*").From("items")
		fields := query.Registry{
			"ids":  {Column: "id", Type: query.Integer, Operators: []query.Operator{query.In}},
			"when": {Column: "created_at", Type: query.DateTime, Operators: []query.Operator{query.Between}},
		}
		filtered, err := query.WithFilters(base, fields, query.Filters{
			"ids":  {{Op: query.In, Value: []any{json.Number("9007199254740993"), "2"}}},
			"when": {{Op: query.Between, Value: query.Range{From: from.Format(time.RFC3339Nano), To: to.Format(time.RFC3339Nano)}}},
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

func TestPreparationDoesNotChangeCallerValues(t *testing.T) {
	for _, flavor := range []sqlbuilder.Flavor{sqlbuilder.PostgreSQL, sqlbuilder.MySQL} {
		base := flavor.NewSelectBuilder().Select("x").From("items")
		base.Where(base.Equal("tenant_id", 42))
		beforeSQL, beforeArgs := base.Build()
		fields := query.Registry{"x": {Column: "x", Type: query.Integer, Operators: []query.Operator{query.In, query.NotIn, query.Between, query.RangeOp, query.NotRange}}}
		filters := query.Filters{"x": {
			{Op: query.In, Value: []any{"1", int16(2), json.Number("3")}},
			{Op: query.NotIn, Value: []any{float32(4.5)}},
			{Op: query.Between, Value: query.Range{From: "5", To: 6.5}},
			{Op: query.RangeOp, Value: query.Range{From: "7"}},
			{Op: query.RangeOp, Value: query.Range{To: "8"}},
			{Op: query.NotRange, Value: query.Range{From: "9", To: 10}},
			{Op: query.NotRange, Value: query.Range{From: "11"}},
			{Op: query.NotRange, Value: query.Range{To: "12"}},
		}}
		original := query.Filters{"x": {
			{Op: query.In, Value: []any{"1", int16(2), json.Number("3")}},
			{Op: query.NotIn, Value: []any{float32(4.5)}},
			{Op: query.Between, Value: query.Range{From: "5", To: 6.5}},
			{Op: query.RangeOp, Value: query.Range{From: "7"}},
			{Op: query.RangeOp, Value: query.Range{To: "8"}},
			{Op: query.NotRange, Value: query.Range{From: "9", To: 10}},
			{Op: query.NotRange, Value: query.Range{From: "11"}},
			{Op: query.NotRange, Value: query.Range{To: "12"}},
		}}
		for i := 0; i < 2; i++ {
			filtered, err := query.WithFilters(base, fields, filters)
			if err != nil {
				t.Fatal(err)
			}
			_, args := filtered.Build()
			want := []any{42, int64(1), int16(2), int64(3), float32(4.5), int64(5), 6.5, int64(7), int64(8), int64(9), 10, int64(11), int64(12)}
			if !reflect.DeepEqual(args, want) || !reflect.DeepEqual(filters, original) {
				t.Fatalf("args %#v; input changed: %v", args, !reflect.DeepEqual(filters, original))
			}
		}
		// Failure after preparing the first element must not rewrite the list.
		items := []any{"1", "bad"}
		filtered, err := query.WithFilters(base, fields, query.Filters{"x": {{Op: query.In, Value: items}}})
		if filtered != nil || err == nil || !reflect.DeepEqual(items, []any{"1", "bad"}) {
			t.Fatalf("failed preparation changed input: %#v, %v", items, err)
		}
		afterSQL, afterArgs := base.Build()
		if afterSQL != beforeSQL || !reflect.DeepEqual(afterArgs, beforeArgs) {
			t.Fatal("preparation changed base query")
		}
	}
}

func TestJSONFilterValuesArePrepared(t *testing.T) {
	const input = `{
		"age": [{"op": "gte", "value": 25}],
		"enabled": [{"op": "eq", "value": "true"}],
		"ip": [{"op": "not_contains", "value": "192.168."}],
		"ports": [{"op": "in", "value": ["22", 443]}],
		"score": [{"op": "gte", "value": "7.5"}],
		"since": [{"op": "gte", "value": "2026-10-21T12:30:00Z"}]
	}`
	fields := query.Registry{
		"age":     {Column: "age", Type: query.Integer, Operators: []query.Operator{query.GTE}},
		"enabled": {Column: "enabled", Type: query.Boolean, Operators: []query.Operator{query.EQ}},
		"ip":      {Column: "ips", Type: query.StringArray, Operators: []query.Operator{query.NotContains}},
		"ports":   {Column: "ports", Type: query.IntegerArray, Operators: []query.Operator{query.In}},
		"score":   {Column: "score", Type: query.Number, Operators: []query.Operator{query.GTE}},
		"since":   {Column: "created_at", Type: query.DateTime, Operators: []query.Operator{query.GTE}},
	}
	for _, useNumber := range []bool{false, true} {
		decoder := json.NewDecoder(strings.NewReader(input))
		if useNumber {
			decoder.UseNumber()
		}
		var filters query.Filters
		if err := decoder.Decode(&filters); err != nil {
			t.Fatal(err)
		}
		base := sqlbuilder.PostgreSQL.NewSelectBuilder().Select("id").From("items")
		filtered, err := query.WithFilters(base, fields, filters)
		if err != nil {
			t.Fatal(err)
		}
		_, args := filtered.Build()
		var age, port any = float64(25), float64(443)
		if useNumber {
			age, port = int64(25), int64(443)
		}
		want := []any{age, true, "%192.168.%", int64(22), port, 7.5, time.Date(2026, 10, 21, 12, 30, 0, 0, time.UTC)}
		if !reflect.DeepEqual(args, want) {
			t.Fatalf("UseNumber=%v: got %#v, want %#v", useNumber, args, want)
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
