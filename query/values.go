package query

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
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

// prepareCondition handles the operator's value shape after checkOperator.
// It returns a copy and never writes into caller-owned lists or ranges.
func prepareCondition(typ ValueType, condition Condition) (Condition, error) {
	var err error
	switch condition.Op {
	case Empty, NotEmpty:
		if condition.Value != nil {
			return Condition{}, fmt.Errorf("%s takes no value", condition.Op)
		}
	case In, NotIn:
		items, ok := condition.Value.([]any)
		if !ok || len(items) == 0 {
			return Condition{}, fmt.Errorf("in/not_in requires a nonempty []any")
		}
		element := typ
		if scalar, ok := arrayElement(typ); ok {
			element = scalar
		}
		prepared := make([]any, len(items))
		for i, item := range items {
			prepared[i], err = prepareValue(element, item)
			if err != nil {
				return Condition{}, fmt.Errorf("value[%d]: %w", i, err)
			}
		}
		condition.Value = prepared
	case Between, RangeOp, NotRange:
		bounds, ok := condition.Value.(Range)
		if !ok {
			return Condition{}, fmt.Errorf("%s requires query.Range", condition.Op)
		}
		if bounds.From == nil && bounds.To == nil || condition.Op == Between && (bounds.From == nil || bounds.To == nil) {
			return Condition{}, fmt.Errorf("between needs both bounds; range needs at least one")
		}
		if bounds.From != nil {
			bounds.From, err = prepareValue(typ, bounds.From)
			if err != nil {
				return Condition{}, fmt.Errorf("from: %w", err)
			}
		}
		if bounds.To != nil {
			bounds.To, err = prepareValue(typ, bounds.To)
			if err != nil {
				return Condition{}, fmt.Errorf("to: %w", err)
			}
		}
		condition.Value = bounds
	case Contains, NotContains:
		condition.Value, err = prepareValue(String, condition.Value)
	default:
		if condition.Value == nil && (condition.Op == EQ || condition.Op == NE) {
			return condition, nil
		}
		condition.Value, err = prepareValue(typ, condition.Value)
	}
	return condition, err
}

// prepareValue parses supported text representations and preserves Go numbers.
// It never stringifies objects or unwraps pointers/Valuers.
func prepareValue(typ ValueType, raw any) (any, error) {
	if raw == nil {
		return nil, fmt.Errorf("null is not allowed here")
	}

	var err error
	switch typ {
	case String, Enum, UUID:
		if value, ok := raw.(string); ok {
			return value, nil
		}
	case Integer, Counter, Number:
		input := raw
		if number, ok := raw.(json.Number); ok {
			input = string(number)
		}
		switch value := input.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			return raw, nil
		case float32:
			if !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0) {
				return raw, nil
			}
			err = fmt.Errorf("number must be finite")
		case float64:
			if !math.IsNaN(value) && !math.IsInf(value, 0) {
				return raw, nil
			}
			err = fmt.Errorf("number must be finite")
		case string:
			if typ == Number {
				var parsed float64
				parsed, err = strconv.ParseFloat(value, 64)
				if err == nil {
					if !math.IsNaN(parsed) && !math.IsInf(parsed, 0) {
						return parsed, nil
					}
					err = fmt.Errorf("number must be finite")
				}
			} else {
				var parsed int64
				parsed, err = strconv.ParseInt(value, 10, 64)
				if err == nil {
					return parsed, nil
				}
			}
		}
	case Boolean:
		switch value := raw.(type) {
		case bool:
			return value, nil
		case string:
			var parsed bool
			parsed, err = strconv.ParseBool(value)
			if err == nil {
				return parsed, nil
			}
		}
	case Date, DateTime:
		switch value := raw.(type) {
		case time.Time:
			return value, nil
		case string:
			layout := time.RFC3339Nano
			if typ == Date {
				layout = time.DateOnly
			}
			var parsed time.Time
			parsed, err = time.Parse(layout, value)
			if err == nil {
				return parsed, nil
			}
		}
	case Bytes:
		if value, ok := raw.([]byte); ok && value != nil {
			return value, nil
		}
	}
	if err != nil {
		return nil, fmt.Errorf("expected %s, got %T: %w", typ, raw, err)
	}
	return nil, fmt.Errorf("expected %s, got %T", typ, raw)
}
