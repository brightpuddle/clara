package orchestrator

import (
	"fmt"

	"github.com/cockroachdb/errors"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"gopkg.in/yaml.v3"
)

// YAMLModule is a Starlark module that exposes yaml.encode and yaml.decode,
// mirroring the interface of the built-in json module from go.starlark.net/lib/json.
//
// Usage in Starlark:
//
//	data = yaml.decode(raw_string)   # string → dict/list/...
//	text = yaml.encode(data)         # dict/list/... → string
var YAMLModule starlark.Value = &starlarkstruct.Module{
	Name: "yaml",
	Members: starlark.StringDict{
		"decode": starlark.NewBuiltin("yaml.decode", yamlDecode),
		"encode": starlark.NewBuiltin("yaml.encode", yamlEncode),
	},
}

// yamlDecode implements yaml.decode(s) → value.
// Accepts a single string argument and returns the parsed Starlark value.
func yamlDecode(
	_ *starlark.Thread,
	_ *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var s starlark.String
	if err := starlark.UnpackPositionalArgs("yaml.decode", args, kwargs, 1, &s); err != nil {
		return nil, err
	}

	var raw any
	if err := yaml.Unmarshal([]byte(s.GoString()), &raw); err != nil {
		return nil, errors.Wrapf(err, "yaml.decode")
	}

	result, err := GoToStarlark(normalizeYAML(raw))
	if err != nil {
		return nil, errors.Wrap(err, "yaml.decode: convert to starlark")
	}
	return result, nil
}

// yamlEncode implements yaml.encode(v) → string.
// Accepts a single Starlark value and returns its YAML representation.
func yamlEncode(
	_ *starlark.Thread,
	_ *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var v starlark.Value
	if err := starlark.UnpackPositionalArgs("yaml.encode", args, kwargs, 1, &v); err != nil {
		return nil, err
	}

	go_, err := StarlarkValueToGo(v)
	if err != nil {
		return nil, errors.Wrap(err, "yaml.encode: convert from starlark")
	}

	out, err := yaml.Marshal(go_)
	if err != nil {
		return nil, errors.Wrap(err, "yaml.encode")
	}
	return starlark.String(string(out)), nil
}

// normalizeYAML recursively converts the output of yaml.Unmarshal into types
// that GoToStarlark can handle. The YAML library may produce map[string]any,
// map[any]any (for non-string-keyed maps), []any, scalars, and nil.
func normalizeYAML(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			out[k] = normalizeYAML(child)
		}
		return out
	case map[any]any:
		// YAML allows non-string keys; coerce them to strings.
		out := make(map[string]any, len(val))
		for k, child := range val {
			out[fmt.Sprintf("%v", k)] = normalizeYAML(child)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, child := range val {
			out[i] = normalizeYAML(child)
		}
		return out
	default:
		return val
	}
}
