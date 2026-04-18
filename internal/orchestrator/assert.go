package orchestrator

import (
	"fmt"
	"go.starlark.net/starlark"
)

var AssertModule starlark.Value = &assertModule{}

type assertModule struct{}

func (m *assertModule) String() string        { return "<module assert>" }
func (m *assertModule) Type() string          { return "module" }
func (m *assertModule) Freeze()               {}
func (m *assertModule) Truth() starlark.Bool  { return true }
func (m *assertModule) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable: %s", m.Type()) }

func (m *assertModule) Attr(name string) (starlark.Value, error) {
	switch name {
	case "eq":
		return starlark.NewBuiltin("eq", assertEq), nil
	case "neq":
		return starlark.NewBuiltin("neq", assertNeq), nil
	case "true":
		return starlark.NewBuiltin("true", assertTrue), nil
	case "false":
		return starlark.NewBuiltin("false", assertFalse), nil
	case "fails":
		return starlark.NewBuiltin("fails", assertFails), nil
	default:
		return nil, nil
	}
}

func (m *assertModule) AttrNames() []string {
	return []string{"eq", "neq", "true", "false", "fails"}
}

func assertEq(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x, y starlark.Value
	if err := starlark.UnpackArgs("eq", args, kwargs, "x", &x, "y", &y); err != nil {
		return nil, err
	}
	if ok, err := starlark.Equal(x, y); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("assert.eq failed: %v != %v", x, y)
	}
	return starlark.None, nil
}

func assertNeq(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x, y starlark.Value
	if err := starlark.UnpackArgs("neq", args, kwargs, "x", &x, "y", &y); err != nil {
		return nil, err
	}
	if ok, err := starlark.Equal(x, y); err != nil {
		return nil, err
	} else if ok {
		return nil, fmt.Errorf("assert.neq failed: %v == %v", x, y)
	}
	return starlark.None, nil
}

func assertTrue(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var cond starlark.Value
	if err := starlark.UnpackArgs("true", args, kwargs, "cond", &cond); err != nil {
		return nil, err
	}
	if !cond.Truth() {
		return nil, fmt.Errorf("assert.true failed: expected True, got False")
	}
	return starlark.None, nil
}

func assertFalse(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var cond starlark.Value
	if err := starlark.UnpackArgs("false", args, kwargs, "cond", &cond); err != nil {
		return nil, err
	}
	if cond.Truth() {
		return nil, fmt.Errorf("assert.false failed: expected False, got True")
	}
	return starlark.None, nil
}

func assertFails(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var f starlark.Callable
	if err := starlark.UnpackArgs("fails", args, kwargs, "f", &f); err != nil {
		return nil, err
	}
	_, err := starlark.Call(thread, f, nil, nil)
	if err == nil {
		return nil, fmt.Errorf("assert.fails failed: expected function to fail but it succeeded")
	}
	return starlark.None, nil
}
