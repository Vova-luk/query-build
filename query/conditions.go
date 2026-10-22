package query

import (
	"fmt"
	"strings"

	sqlbuilder "github.com/huandu/go-sqlbuilder"
)

func compile(sb *sqlbuilder.SelectBuilder, column string, typ ValueType, condition Condition) (string, error) {
	if err := checkOperator(typ, condition.Op); err != nil {
		return "", err
	}
	if element, ok := arrayElement(typ); ok {
		return compileArray(sb, column, element, condition)
	}
	switch condition.Op {
	case Empty, NotEmpty:
		if condition.Value != nil {
			return "", fmt.Errorf("%s takes no value", condition.Op)
		}
		if condition.Op == Empty {
			return sb.IsNull(column), nil
		}
		return sb.IsNotNull(column), nil
	case In, NotIn:
		items, err := checkedList(condition.Value, typ)
		if err != nil {
			return "", err
		}
		return membership(sb, column, typ, items, condition.Op == NotIn), nil
	case Contains, NotContains:
		if err := validateValue(condition.Value, String); err != nil {
			return "", err
		}
		return substring(sb, column, condition.Value.(string), condition.Op == NotContains), nil
	case Between, RangeOp, NotRange:
		bounds, ok := condition.Value.(Range)
		if !ok {
			return "", fmt.Errorf("%s requires query.Range", condition.Op)
		}
		if bounds.From == nil && bounds.To == nil || condition.Op == Between && (bounds.From == nil || bounds.To == nil) {
			return "", fmt.Errorf("between needs both bounds; range needs at least one")
		}
		if bounds.From != nil {
			if err := validateValue(bounds.From, typ); err != nil {
				return "", fmt.Errorf("from: %w", err)
			}
		}
		if bounds.To != nil {
			if err := validateValue(bounds.To, typ); err != nil {
				return "", fmt.Errorf("to: %w", err)
			}
		}
		switch {
		case bounds.From != nil && bounds.To != nil:
			if condition.Op == NotRange {
				return sb.NotBetween(column, bounds.From, bounds.To), nil
			}
			return sb.Between(column, bounds.From, bounds.To), nil
		case bounds.From != nil:
			if condition.Op == NotRange {
				return sb.LessThan(column, bounds.From), nil
			}
			return sb.GreaterEqualThan(column, bounds.From), nil
		default:
			if condition.Op == NotRange {
				return sb.GreaterThan(column, bounds.To), nil
			}
			return sb.LessEqualThan(column, bounds.To), nil
		}
	case EQ, NE, GT, GTE, LT, LTE, Like, ILike:
		value := condition.Value
		if value == nil {
			switch condition.Op {
			case EQ:
				return sb.IsNull(column), nil
			case NE:
				return sb.IsNotNull(column), nil
			}
		}
		if err := validateValue(value, typ); err != nil {
			return "", err
		}
		if (condition.Op == EQ || condition.Op == NE) && (typ == String || typ == Enum) {
			column = "LOWER(" + textColumn(sb.Flavor(), column, typ) + ")"
			value = sqlbuilder.Build("LOWER($?)", value)
		}
		switch condition.Op {
		case EQ:
			return sb.Equal(column, value), nil
		case NE:
			return sb.NotEqual(column, value), nil
		case GT:
			return sb.GreaterThan(column, value), nil
		case GTE:
			return sb.GreaterEqualThan(column, value), nil
		case LT:
			return sb.LessThan(column, value), nil
		case LTE:
			return sb.LessEqualThan(column, value), nil
		case Like, ILike:
			if condition.Op == Like {
				return sb.Like(column, value), nil
			}
			return sb.ILike(column, value), nil
		}
	}
	return "", fmt.Errorf("unsupported operator %q", condition.Op)
}

func checkOperator(typ ValueType, op Operator) error {
	if _, ok := arrayElement(typ); ok {
		if op == In || op == NotIn || typ == StringArray && (op == Contains || op == NotContains) {
			return nil
		}
		return fmt.Errorf("operator %q is not supported for type %q", op, typ)
	}
	switch op {
	case EQ, NE, In, NotIn:
		return nil
	case Like, ILike, Contains, NotContains:
		if typ == String {
			return nil
		}
	case GT, GTE, LT, LTE:
		if typ == String || typ == Integer || typ == Number || typ == Counter || typ == Date || typ == DateTime {
			return nil
		}
	case Between, RangeOp, NotRange:
		if typ == Integer || typ == Number || typ == Counter || typ == Date || typ == DateTime {
			return nil
		}
	case Empty, NotEmpty:
		if typ == Date || typ == DateTime {
			return nil
		}
	default:
		return fmt.Errorf("unsupported operator %q", op)
	}
	return fmt.Errorf("operator %q is not supported for type %q", op, typ)
}

func checkedList(value any, typ ValueType) ([]any, error) {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return nil, fmt.Errorf("in/not_in requires a nonempty []any")
	}
	for i, item := range items {
		if err := validateValue(item, typ); err != nil {
			return nil, fmt.Errorf("value[%d]: %w", i, err)
		}
	}
	return items, nil
}

// Enum labels are compared as text, including native PostgreSQL enum columns.
func textColumn(flavor sqlbuilder.Flavor, column string, typ ValueType) string {
	if typ == Enum && flavor == sqlbuilder.PostgreSQL {
		return "CAST(" + column + " AS TEXT)"
	}
	return column
}

func membership(sb *sqlbuilder.SelectBuilder, column string, typ ValueType, items []any, negative bool) string {
	if typ == String || typ == Enum {
		column = "LOWER(" + textColumn(sb.Flavor(), column, typ) + ")"
		folded := make([]any, len(items))
		for i, item := range items {
			folded[i] = sqlbuilder.Build("LOWER($?)", item)
		}
		items = folded
	}
	if negative {
		return sb.NotIn(column, items...)
	}
	return sb.In(column, items...)
}

// Escape LIKE metacharacters: Contains searches for a literal substring.
var escapeSubstring = strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")

func substring(sb *sqlbuilder.SelectBuilder, column, value string, negative bool) string {
	pattern := "%" + escapeSubstring.Replace(value) + "%"
	if negative {
		return sb.NotILike(column, pattern) + " ESCAPE '!'"
	}
	return sb.ILike(column, pattern) + " ESCAPE '!'"
}
