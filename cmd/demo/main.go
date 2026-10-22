package main

import (
	"fmt"
	"log"

	"example.com/huandu-v3/query"
	sqlbuilder "github.com/huandu/go-sqlbuilder"
)

func main() {
	// Illustrative schema. Every repository provides its own allowed columns.
	fields := query.Registry{
		"score":  {Column: "a.score", Type: query.Number, Operators: []query.Operator{query.Between, query.GTE, query.LTE, query.RangeOp}},
		"status": {Column: "a.status", Type: query.String, Operators: []query.Operator{query.EQ, query.In}},
		"owner":  {Column: "o.name", Type: query.String, Operators: []query.Operator{query.EQ, query.ILike}},
	}
	// Numeric strings are parsed; Go numbers retain their original types.
	filters := query.Filters{
		"score":  {{Op: query.Between, Value: query.Range{From: "7", To: "9"}}, {Op: query.RangeOp, Value: query.Range{From: 10}}},
		"status": {{Op: query.EQ, Value: "active"}, {Op: query.EQ, Value: "pending"}},
		"owner":  {{Op: query.ILike, Value: "%team%"}},
	}

	for _, flavor := range []sqlbuilder.Flavor{sqlbuilder.PostgreSQL, sqlbuilder.MySQL} {
		base := flavor.NewSelectBuilder().Select("a.id", "a.score", "o.name").From("assets AS a")
		base.JoinWithOption(sqlbuilder.LeftJoin, "owners AS o", base.Equal("a.owner_id", sqlbuilder.Raw("o.id")))
		base.Where(base.Equal("a.tenant_id", 42))

		filtered, err := query.WithFilters(base, fields, filters)
		if err != nil {
			log.Fatal(err)
		}
		// Native methods: independent sort directions and pagination.
		sql, args := filtered.OrderByDesc("a.score").OrderByAsc("a.id").Limit(20).Offset(40).Build()
		fmt.Printf("%s rows:\n%s\nargs: %#v\n\n", flavor, sql, args)

		// Reuse the same base and filters for an aggregate query.
		grouped, err := query.WithFilters(base, fields, filters)
		if err != nil {
			log.Fatal(err)
		}
		grouped.Select("a.status", "COUNT(*) AS total").GroupBy("a.status")
		grouped.Having(grouped.GreaterThan("COUNT(*)", 2))
		sql, args = grouped.OrderByDesc("total").Limit(10).Build()
		fmt.Printf("%s groups:\n%s\nargs: %#v\n\n", flavor, sql, args)
	}
}
