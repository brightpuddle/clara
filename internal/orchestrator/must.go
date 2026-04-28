package orchestrator

import (
	"fmt"
	"go.starlark.net/starlark"
)

var MustModule starlark.Value = &mustModule{}

type mustModule struct{}

func (m *mustModule) String() string        { return "<module must>" }
func (m *mustModule) Type() string          { return "module" }
func (m *mustModule) Freeze()               {}
func (m *mustModule) Truth() starlark.Bool  { return true }
func (m *mustModule) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable: %s", m.Type()) }

func (m *mustModule) Attr(name string) (starlark.Value, error) {
	switch name {
	case "eq":
		return starlark.NewBuiltin("eq", mustEq), nil
	case "neq":
		return starlark.NewBuiltin("neq", mustNeq), nil
	case "true":
		return starlark.NewBuiltin("true", mustTrue), nil
	case "false":
		return starlark.NewBuiltin("false", mustFalse), nil
	case "fails":
		return starlark.NewBuiltin("fails", mustFails), nil
	default:
		return nil, nil
	}
}

func (m *mustModule) AttrNames() []string {
	return []string{"eq", "neq", "true", "false", "fails"}
}

func mustEq(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x, y starlark.Value
	if err := starlark.UnpackArgs("eq", args, kwargs, "x", &x, "y", &y); err != nil {
		return nil, err
	}
	if ok, err := starlark.Equal(x, y); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("must.eq failed: %v != %v", x, y)
	}
	return starlark.None, nil
}

func mustNeq(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x, y starlark.Value
	if err := starlark.UnpackArgs("neq", args, kwargs, "x", &x, "y", &y); err != nil {
		return nil, err
	}
	if ok, err := starlark.Equal(x, y); err != nil {
		return nil, err
	} else if ok {
		return nil, fmt.Errorf("must.neq failed: %v == %v", x, y)
	}
	return starlark.None, nil
}

func mustTrue(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var cond starlark.Value
	if err := starlark.UnpackArgs("true", args, kwargs, "cond", &cond); err != nil {
		return nil, err
	}
	if !cond.Truth() {
		return nil, fmt.Errorf("must.true failed: expected True, got False")
	}
	return starlark.None, nil
}

func mustFalse(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var cond starlark.Value
	if err := starlark.UnpackArgs("false", args, kwargs, "cond", &cond); err != nil {
		return nil, err
	}
	if cond.Truth() {
		return nil, fmt.Errorf("must.false failed: expected False, got True")
	}
	return starlark.None, nil
}

func mustFails(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var f starlark.Callable
	if err := starlark.UnpackArgs("fails", args, kwargs, "f", &f); err != nil {
		return nil, err
	}
	_, err := starlark.Call(thread, f, nil, nil)
	if err == nil {
		return nil, fmt.Errorf("must.fails failed: expected function to fail but it succeeded")
	}
	return starlark.None, nil
}
