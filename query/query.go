package query

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	sqlbuilder "github.com/huandu/go-sqlbuilder"
)

// WithFilters returns an independent copy of base with additional WHERE
// conditions. Mandatory base conditions remain ANDed with the entire filter.
// Filters never accumulate in base, even when a request fails validation.
// Choose the flavor before calling; do not mutate base during cloning.
// Use the returned builder's native Limit/Offset/GroupBy/Having/OrderBy methods.
func WithFilters(base *sqlbuilder.SelectBuilder, fields Registry, filters Filters) (*sqlbuilder.SelectBuilder, error) {
	if base == nil {
		return nil, fmt.Errorf("nil base query")
	}
	if base.Flavor() != sqlbuilder.PostgreSQL && base.Flavor() != sqlbuilder.MySQL {
		return nil, fmt.Errorf("unsupported SQL flavor %s: use PostgreSQL or MySQL (MariaDB)", base.Flavor())
	}
	sb := base.Clone()
	// Map iteration must not change SQL text, parameter order or error order.
	names := make([]string, 0, len(filters))
	for name := range filters {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		field, ok := fields[name]
		if !ok {
			return nil, fmt.Errorf("unknown filter field %q", name)
		}
		column, err := quoteColumn(sb.Flavor(), field.Column)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", name, err)
		}
		if !validType(field.Type) {
			return nil, fmt.Errorf("field %q: missing or unsupported value type %q", name, field.Type)
		}
		conditions := filters[name]
		if len(conditions) == 0 {
			return nil, fmt.Errorf("field %q needs at least one condition", name)
		}
		alternatives := make([]string, 0, len(conditions))
		for i, condition := range conditions {
			if !slices.Contains(field.Operators, condition.Op) {
				return nil, fmt.Errorf("field %q condition[%d]: operator %q is not allowed", name, i, condition.Op)
			}
			expr, err := compile(sb, column, field.Type, condition)
			if err != nil {
				return nil, fmt.Errorf("field %q condition[%d]: %w", name, i, err)
			}
			alternatives = append(alternatives, expr)
		}
		if len(alternatives) == 1 {
			sb.Where(alternatives[0])
		} else {
			sb.Where(sb.Or(alternatives...))
		}
	}
	return sb, nil
}

func quoteColumn(flavor sqlbuilder.Flavor, column string) (string, error) {
	parts := strings.Split(column, ".")
	for i, part := range parts {
		if part == "" {
			return "", fmt.Errorf("invalid column %q", column)
		}
		for j, ch := range []byte(part) {
			if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch == '_' || j > 0 && ch >= '0' && ch <= '9' {
				continue
			}
			return "", fmt.Errorf("invalid column %q: expected a plain dotted identifier", column)
		}
		parts[i] = flavor.Quote(part)
	}
	return strings.Join(parts, "."), nil
}
