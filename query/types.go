// Package query adds field-based filters to huandu/go-sqlbuilder SELECTs.
// Different fields always use AND; conditions on one field always use OR.
// SQL construction, pagination, grouping, joins and ordering stay in sqlbuilder.
package query

// ValueType is the expected application value type, configured by the server.
// It does not discover the database schema or encode SQL storage constraints.
type ValueType string

const (
	String       ValueType = "string"
	Integer      ValueType = "integer"
	Number       ValueType = "number"
	Boolean      ValueType = "boolean"
	Date         ValueType = "date"
	DateTime     ValueType = "datetime"
	Bytes        ValueType = "bytes"
	Enum         ValueType = "enum"
	UUID         ValueType = "uuid"
	Counter      ValueType = "counter"
	StringArray  ValueType = "string[]"
	EnumArray    ValueType = "enum[]"
	UUIDArray    ValueType = "uuid[]"
	IntegerArray ValueType = "int[]"
)

type Operator string

const (
	EQ          Operator = "eq"
	NE          Operator = "ne"
	GT          Operator = "gt"
	GTE         Operator = "gte"
	LT          Operator = "lt"
	LTE         Operator = "lte"
	In          Operator = "in"
	NotIn       Operator = "not_in"
	Contains    Operator = "contains"
	NotContains Operator = "not_contains"
	Like        Operator = "like"
	ILike       Operator = "ilike"
	Between     Operator = "between"
	RangeOp     Operator = "range"
	NotRange    Operator = "nrange"
	Empty       Operator = "empty"
	NotEmpty    Operator = "nempty"
)

// Condition belongs to the field containing it in Filters.
// Values are prepared according to Field.Type: integer/number/bool/date strings
// are parsed, and finite Go numeric values keep their original type and value.
// Integer, Counter and Number all accept both integer and floating-point values.
// IN takes []any; BETWEEN/range takes Range. Their elements/bounds are prepared
// individually. Strings are never inferred from numbers or arbitrary objects.
// Pointers and driver.Valuer are not unwrapped.
// nil means SQL NULL for EQ/NE. Empty/NotEmpty take no value.
// Array IN accepts []any of element values; array Contains accepts a substring.
type Condition struct {
	Op    Operator
	Value any
}

// Filters groups alternatives by public field name. An empty map means no
// filters; a present field must have at least one condition.
type Filters map[string][]Condition

// Range has inclusive bounds. Between requires both; RangeOp also accepts
// just From (>=) or just To (<=). nil denotes an absent endpoint.
type Range struct {
	From any
	To   any
}

// Field is server-owned configuration. Column is a plain, possibly dotted SQL
// identifier, not an expression. Type is required for each filtered field.
// Only explicitly listed operators compatible with Type are allowed.
type Field struct {
	Column    string
	Type      ValueType
	Operators []Operator
}

// Registry maps public field names to trusted columns, including JOIN aliases.
// Keep it unchanged while it is in use. It does not describe tables or joins.
type Registry map[string]Field
