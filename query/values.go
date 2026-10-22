package query

import (
	"fmt"
	"math"
	"time"
)

func validType(typ ValueType) bool {
	switch typ {
	case String, Integer, Number, Boolean, Date, DateTime, Bytes, Enum, UUID, Counter:
		return true
	}
	_, ok := arrayElement(typ)
	return ok
}

// validateValue only checks prepared values; it never converts or unwraps them.
// NULL is handled separately for EQ/NE in compile.
func validateValue(value any, typ ValueType) error {
	if value == nil {
		return fmt.Errorf("null is not allowed here")
	}

	valid := false
	switch typ {
	case String, Enum, UUID:
		_, valid = value.(string)
	case Integer, Counter:
		_, valid = value.(int64)
	case Number:
		v, ok := value.(float64)
		valid = ok && !math.IsNaN(v) && !math.IsInf(v, 0)
	case Boolean:
		_, valid = value.(bool)
	case Bytes:
		v, ok := value.([]byte)
		valid = ok && v != nil
	case Date, DateTime:
		_, valid = value.(time.Time)
	}
	if !valid {
		return fmt.Errorf("expected %s, got %T", typ, value)
	}
	return nil
}
