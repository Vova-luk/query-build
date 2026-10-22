package main

import (
	"fmt"
	"log"
	"time"

	"example.com/huandu-v3/query"
	sqlbuilder "github.com/huandu/go-sqlbuilder"
)

func main() {
	// Illustrative PostgreSQL schema: tags text[], owners uuid[], ports integer[].
	fields := query.Registry{
		"name":            {Column: "a.name", Type: query.String, Operators: []query.Operator{query.EQ, query.NE, query.Contains, query.NotContains}},
		"importance":      {Column: "a.importance", Type: query.Enum, Operators: []query.Operator{query.In, query.NotIn}},
		"ip":              {Column: "a.tags", Type: query.StringArray, Operators: []query.Operator{query.Contains, query.NotContains, query.In, query.NotIn}},
		"owners":          {Column: "a.owners", Type: query.UUIDArray, Operators: []query.Operator{query.In, query.NotIn}},
		"ports":           {Column: "a.ports", Type: query.IntegerArray, Operators: []query.Operator{query.In, query.NotIn}},
		"lastSeenOnline":  {Column: "a.last_seen", Type: query.DateTime, Operators: []query.Operator{query.RangeOp, query.NotRange, query.Empty, query.NotEmpty}},
		"vulnerabilities": {Column: "a.vulnerabilities", Type: query.Counter, Operators: []query.Operator{query.RangeOp, query.NotRange}},
	}
	filters := query.Filters{
		"name":            {{Op: query.NotContains, Value: "test_"}},
		"importance":      {{Op: query.In, Value: []any{"critical", "high"}}},
		"ip":              {{Op: query.Contains, Value: "192.168."}},
		"owners":          {{Op: query.In, Value: []any{"00000000-0000-0000-0000-000000000001"}}},
		"ports":           {{Op: query.NotIn, Value: []any{int64(23), int64(3389)}}},
		"lastSeenOnline":  {{Op: query.RangeOp, Value: query.Range{From: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}}},
		"vulnerabilities": {{Op: query.RangeOp, Value: query.Range{From: int64(10)}}},
	}
	base := sqlbuilder.PostgreSQL.NewSelectBuilder().Select("a.id", "a.name").From("assets AS a")
	base.Where(base.Equal("a.tenant_id", int64(42)))
	sb, err := query.WithFilters(base, fields, filters)
	if err != nil {
		log.Fatal(err)
	}
	sql, args := sb.OrderByAsc("a.id").Limit(50).Offset(0).Build()
	fmt.Printf("%s\nargs: %#v\n", sql, args)
}
