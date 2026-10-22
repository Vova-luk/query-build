package query

import sqlbuilder "github.com/huandu/go-sqlbuilder"

func arrayElement(typ ValueType) (ValueType, bool) {
	switch typ {
	case StringArray:
		return String, true
	case EnumArray:
		return Enum, true
	case UUIDArray:
		return UUID, true
	case IntegerArray:
		return Integer, true
	}
	return "", false
}

// Array values are expanded by PostgreSQL, not serialized by a Go driver.
// EXISTS implements "at least one match"; NOT EXISTS implements "no matches".
// NULL elements do not match; NULL and empty arrays both have no matches.
func compileArray(sb *sqlbuilder.SelectBuilder, column string, element ValueType, condition Condition) string {
	sub := sqlbuilder.PostgreSQL.NewSelectBuilder().Select("1").
		From("unnest(" + column + `) AS "__v3_element"("value")`)
	const valueColumn = `"__v3_element"."value"`
	switch condition.Op {
	case In, NotIn:
		items := condition.Value.([]any)
		sub.Where(membership(sub, valueColumn, element, items, false))
	case Contains, NotContains:
		sub.Where(substring(sub, valueColumn, condition.Value.(string), false))
	}
	if condition.Op == NotIn || condition.Op == NotContains {
		return sb.NotExists(sub)
	}
	return sb.Exists(sub)
}
