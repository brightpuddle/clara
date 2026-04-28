package orchestrator

import (
	"reflect"
	"sort"
	"time"

	"github.com/cockroachdb/errors"
	"go.starlark.net/starlark"
)

func StarlarkValueToGo(value starlark.Value) (any, error) {
	switch v := value.(type) {
	case starlark.NoneType:
		return nil, nil
	case starlark.Bool:
		return bool(v), nil
	case starlark.String:
		return v.GoString(), nil
	case starlark.Int:
		if i, ok := v.Int64(); ok {
			return i, nil
		}
		return nil, errors.Newf("integer %s exceeds int64", v.String())
	case starlark.Float:
		return float64(v), nil
	case *starlark.List:
		items := make([]any, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			item, err := StarlarkValueToGo(v.Index(i))
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, nil
	case starlark.Tuple:
		items := make([]any, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			item, err := StarlarkValueToGo(v.Index(i))
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, nil
	case *starlark.Dict:
		items := make(map[string]any, v.Len())
		for _, item := range v.Items() {
			key, ok := starlark.AsString(item[0])
			if !ok {
				return nil, errors.New("starlark dict keys must be strings")
			}
			converted, err := StarlarkValueToGo(item[1])
			if err != nil {
				return nil, err
			}
			items[key] = converted
		}
		return items, nil
	default:
		return nil, errors.Newf("unsupported starlark value %s", value.Type())
	}
}

func GoToStarlark(value any) (starlark.Value, error) {
	switch v := value.(type) {
	case nil:
		return starlark.None, nil
	case bool:
		return starlark.Bool(v), nil
	case string:
		return starlark.String(v), nil
	case time.Time:
		return starlark.String(v.UTC().Format(time.RFC3339Nano)), nil
	case int:
		return starlark.MakeInt(v), nil
	case int64:
		return starlark.MakeInt64(v), nil
	case int32:
		return starlark.MakeInt64(int64(v)), nil
	case uint:
		return starlark.MakeUint64(uint64(v)), nil
	case uint64:
		return starlark.MakeUint64(v), nil
	case float32:
		return starlark.Float(v), nil
	case float64:
		return starlark.Float(v), nil
	case []any:
		values := make([]starlark.Value, 0, len(v))
		for _, item := range v {
			converted, err := GoToStarlark(item)
			if err != nil {
				return nil, err
			}
			values = append(values, converted)
		}
		return starlark.NewList(values), nil
	case []map[string]any:
		values := make([]starlark.Value, 0, len(v))
		for _, item := range v {
			converted, err := GoToStarlark(item)
			if err != nil {
				return nil, err
			}
			values = append(values, converted)
		}
		return starlark.NewList(values), nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		dict := starlark.NewDict(len(v))
		for _, key := range keys {
			converted, err := GoToStarlark(v[key])
			if err != nil {
				return nil, err
			}
			if err := dict.SetKey(starlark.String(key), converted); err != nil {
				return nil, errors.Wrapf(err, "set dict key %q", key)
			}
		}
		return dict, nil
	default:
		rv := reflect.ValueOf(value)
		switch rv.Kind() {
		case reflect.Slice, reflect.Array:
			length := rv.Len()
			values := make([]starlark.Value, 0, length)
			for i := 0; i < length; i++ {
				converted, err := GoToStarlark(rv.Index(i).Interface())
				if err != nil {
					return nil, err
				}
				values = append(values, converted)
			}
			return starlark.NewList(values), nil
		case reflect.Map:
			dict := starlark.NewDict(rv.Len())
			keys := make([]string, 0, rv.Len())
			iter := rv.MapRange()
			for iter.Next() {
				if iter.Key().Kind() != reflect.String {
					return nil, errors.New("go map keys must be strings")
				}
				keys = append(keys, iter.Key().String())
			}
			sort.Strings(keys)
			for _, key := range keys {
				converted, err := GoToStarlark(rv.MapIndex(reflect.ValueOf(key)).Interface())
				if err != nil {
					return nil, err
				}
				if err := dict.SetKey(starlark.String(key), converted); err != nil {
					return nil, err
				}
			}
			return dict, nil
		case reflect.Int8, reflect.Int16:
			return starlark.MakeInt64(rv.Int()), nil
		case reflect.Uint8, reflect.Uint16, reflect.Uint32:
			return starlark.MakeUint64(rv.Uint()), nil
		case reflect.Float64, reflect.Float32:
			return starlark.Float(rv.Float()), nil
		}
		return nil, errors.Newf("unsupported go value %T", value)
	}
}
