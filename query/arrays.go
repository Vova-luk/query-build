package query

import (
	"fmt"

	sqlbuilder "github.com/huandu/go-sqlbuilder"
)

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
func compileArray(sb *sqlbuilder.SelectBuilder, column string, element ValueType, condition Condition) (string, error) {
	if sb.Flavor() != sqlbuilder.PostgreSQL {
		return "", fmt.Errorf("native array filters require PostgreSQL")
	}
	sub := sqlbuilder.PostgreSQL.NewSelectBuilder().Select("1").
		From("unnest(" + column + `) AS "__v3_element"("value")`)
	const valueColumn = `"__v3_element"."value"`
	switch condition.Op {
	case In, NotIn:
		items, err := checkedList(condition.Value, element)
		if err != nil {
			return "", err
		}
		sub.Where(membership(sub, valueColumn, element, items, false))
	case Contains, NotContains:
		if err := validateValue(condition.Value, String); err != nil {
			return "", err
		}
		sub.Where(substring(sub, valueColumn, condition.Value.(string), false))
	}
	if condition.Op == NotIn || condition.Op == NotContains {
		return sb.NotExists(sub), nil
	}
	return sb.Exists(sub), nil
}
