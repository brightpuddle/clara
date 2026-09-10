// Package trigger implements event, schedule, and worker triggers with smart matching rules.
package trigger

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
)

// Op represents the comparison operator for a rule clause.
type Op string

const (
	OpEquals      Op = "equals"
	OpNotEquals   Op = "not_equals"
	OpContains    Op = "contains"
	OpNotContains Op = "not_contains"
	OpStartsWith  Op = "starts_with"
	OpEndsWith    Op = "ends_with"
	OpRegex       Op = "regex"
	OpIn          Op = "in"
	OpNotIn       Op = "not_in"
	OpGt          Op = "gt"
	OpGte         Op = "gte"
	OpLt          Op = "lt"
	OpLte         Op = "lte"
	OpExists      Op = "exists"
	OpNotExists   Op = "not_exists"
)

// Rule represents a matching criteria node.
// It can be a leaf comparison (Field + Op + Value) or a composite (And / Or / Not).
type Rule struct {
	// Composite clauses
	And []*Rule `json:"and,omitempty" yaml:"and,omitempty"`
	Or  []*Rule `json:"or,omitempty"  yaml:"or,omitempty"`
	Not *Rule   `json:"not,omitempty" yaml:"not,omitempty"`

	// Leaf condition
	Field string `json:"field,omitempty" yaml:"field,omitempty"`
	Op    Op     `json:"op,omitempty"    yaml:"op,omitempty"`
	Value any    `json:"value,omitempty" yaml:"value,omitempty"`
}

// Match evaluates whether the rule matches against the provided data context.
// Data is typically a map[string]any representing the CloudEvent.
func (r *Rule) Match(data any) (bool, error) {
	if r == nil {
		return true, nil
	}

	// 1. Evaluate composite AND
	if len(r.And) > 0 {
		for _, sub := range r.And {
			matched, err := sub.Match(data)
			if err != nil {
				return false, err
			}
			if !matched {
				return false, nil
			}
		}
		// If there are no other top-level fields, AND succeeded
		if len(r.Or) == 0 && r.Not == nil && r.Field == "" {
			return true, nil
		}
	}

	// 2. Evaluate composite OR
	if len(r.Or) > 0 {
		var matchedAny bool
		for _, sub := range r.Or {
			matched, err := sub.Match(data)
			if err != nil {
				return false, err
			}
			if matched {
				matchedAny = true
				break
			}
		}
		if !matchedAny {
			return false, nil
		}
		// If there are no other top-level fields, OR succeeded
		if r.Not == nil && r.Field == "" {
			return true, nil
		}
	}

	// 3. Evaluate composite NOT
	if r.Not != nil {
		matched, err := r.Not.Match(data)
		if err != nil {
			return false, err
		}
		if matched {
			return false, nil
		}
		if r.Field == "" {
			return true, nil
		}
	}

	// 4. Evaluate leaf condition if Field is set
	if r.Field != "" {
		return r.evalLeaf(data)
	}

	return true, nil
}

// ExtractField extracts a nested field from a map or struct using dot notation.
// Example: "data.headers.from" or "type".
func ExtractField(data any, path string) (any, bool) {
	if data == nil || path == "" {
		return nil, false
	}

	parts := strings.Split(path, ".")
	current := data

	for _, part := range parts {
		if current == nil {
			return nil, false
		}

		val := reflect.ValueOf(current)
		// Handle pointer dereference
		if val.Kind() == reflect.Ptr || val.Kind() == reflect.Interface {
			if val.IsNil() {
				return nil, false
			}
			current = val.Elem().Interface()
			val = reflect.ValueOf(current)
		}

		switch val.Kind() {
		case reflect.Map:
			keyVal := reflect.ValueOf(part)
			mapVal := val.MapIndex(keyVal)
			if !mapVal.IsValid() {
				// Try case-insensitive or string fallback if map has interface keys
				found := false
				for _, k := range val.MapKeys() {
					if fmt.Sprintf("%v", k.Interface()) == part {
						mapVal = val.MapIndex(k)
						found = true
						break
					}
				}
				if !found {
					return nil, false
				}
			}
			current = mapVal.Interface()

		case reflect.Struct:
			field := val.FieldByName(part)
			if !field.IsValid() {
				// Try case-insensitive match for struct fields
				typ := val.Type()
				found := false
				for i := 0; i < typ.NumField(); i++ {
					f := typ.Field(i)
					if strings.EqualFold(f.Name, part) || f.Tag.Get("json") == part {
						field = val.Field(i)
						found = true
						break
					}
				}
				if !found {
					return nil, false
				}
			}
			current = field.Interface()

		case reflect.Slice, reflect.Array:
			// Allow numeric indexing for slices: "items.0.name"
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= val.Len() {
				return nil, false
			}
			current = val.Index(idx).Interface()

		default:
			return nil, false
		}
	}

	return current, true
}

func (r *Rule) evalLeaf(data any) (bool, error) {
	val, exists := ExtractField(data, r.Field)

	op := r.Op
	if op == "" {
		op = OpEquals
	}

	switch op {
	case OpExists:
		return exists && val != nil, nil

	case OpNotExists:
		return !exists || val == nil, nil
	}

	if !exists {
		// If field does not exist and op is not_equals or not_contains, it may be true
		if op == OpNotEquals || op == OpNotIn || op == OpNotContains {
			return true, nil
		}
		return false, nil
	}

	switch op {
	case OpEquals:
		return compareEquals(val, r.Value), nil

	case OpNotEquals:
		return !compareEquals(val, r.Value), nil

	case OpContains:
		return compareContains(val, r.Value)

	case OpNotContains:
		matched, err := compareContains(val, r.Value)
		return !matched, err

	case OpStartsWith:
		strVal := fmt.Sprintf("%v", val)
		strTarget := fmt.Sprintf("%v", r.Value)
		return strings.HasPrefix(strVal, strTarget), nil

	case OpEndsWith:
		strVal := fmt.Sprintf("%v", val)
		strTarget := fmt.Sprintf("%v", r.Value)
		return strings.HasSuffix(strVal, strTarget), nil

	case OpRegex:
		pattern := fmt.Sprintf("%v", r.Value)
		re, err := regexp.Compile(pattern)
		if err != nil {
			return false, errors.Wrapf(err, "invalid regex pattern: %q", pattern)
		}
		return re.MatchString(fmt.Sprintf("%v", val)), nil

	case OpIn:
		return compareIn(val, r.Value), nil

	case OpNotIn:
		return !compareIn(val, r.Value), nil

	case OpGt, OpGte, OpLt, OpLte:
		return compareNumeric(val, r.Value, op)

	default:
		return false, errors.Newf("unsupported rule operator: %q", op)
	}
}

func compareEquals(a, b any) bool {
	if a == nil || b == nil {
		return a == b
	}

	// Try numeric float comparison
	numA, okA := toFloat(a)
	numB, okB := toFloat(b)
	if okA && okB {
		return numA == numB
	}

	// Try boolean comparison
	boolA, okBA := a.(bool)
	boolB, okBB := b.(bool)
	if okBA && okBB {
		return boolA == boolB
	}

	// String fallback comparison
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func compareContains(container, target any) (bool, error) {
	if container == nil || target == nil {
		return false, nil
	}

	// If container is a string, check substring
	if str, ok := container.(string); ok {
		return strings.Contains(str, fmt.Sprintf("%v", target)), nil
	}

	// If container is slice or array, check element membership
	val := reflect.ValueOf(container)
	if val.Kind() == reflect.Slice || val.Kind() == reflect.Array {
		for i := 0; i < val.Len(); i++ {
			elem := val.Index(i).Interface()
			if compareEquals(elem, target) {
				return true, nil
			}
		}
		return false, nil
	}

	// Fallback to string contains
	return strings.Contains(fmt.Sprintf("%v", container), fmt.Sprintf("%v", target)), nil
}

func compareIn(target, collection any) bool {
	if collection == nil {
		return false
	}

	val := reflect.ValueOf(collection)
	if val.Kind() == reflect.Slice || val.Kind() == reflect.Array {
		for i := 0; i < val.Len(); i++ {
			elem := val.Index(i).Interface()
			if compareEquals(target, elem) {
				return true
			}
		}
		return false
	}

	// If collection is a string, check substring
	if colStr, ok := collection.(string); ok {
		return strings.Contains(colStr, fmt.Sprintf("%v", target))
	}

	return false
}

func compareNumeric(a, b any, op Op) (bool, error) {
	numA, okA := toFloat(a)
	numB, okB := toFloat(b)
	if !okA || !okB {
		return false, errors.Newf("cannot perform numeric comparison %s between %T and %T", op, a, b)
	}

	switch op {
	case OpGt:
		return numA > numB, nil
	case OpGte:
		return numA >= numB, nil
	case OpLt:
		return numA < numB, nil
	case OpLte:
		return numA <= numB, nil
	default:
		return false, errors.Newf("unsupported numeric op: %s", op)
	}
}

func toFloat(v any) (float64, bool) {
	if v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	case string:
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}
