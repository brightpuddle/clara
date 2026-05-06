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

func fail(thread *starlark.Thread, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	stack := thread.CallStack()
	for i := 0; i < len(stack); i++ {
		pos := stack.At(i).Pos
		if pos.Filename() != "" && pos.Filename() != "<builtin>" {
			return fmt.Errorf("%s: %s", pos, msg)
		}
	}
	return fmt.Errorf("%s", msg)
}

func mustEq(
	thread *starlark.Thread,
	b *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var x, y starlark.Value
	var msg string
	if err := starlark.UnpackArgs("eq", args, kwargs, "x", &x, "y", &y, "msg?", &msg); err != nil {
		return nil, err
	}
	if ok, err := starlark.Equal(x, y); err != nil {
		return nil, err
	} else if !ok {
		if msg == "" {
			msg = fmt.Sprintf("%v != %v", x, y)
		}
		return nil, fail(thread, "must.eq failed: %s", msg)
	}
	return starlark.None, nil
}

func mustNeq(
	thread *starlark.Thread,
	b *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var x, y starlark.Value
	var msg string
	if err := starlark.UnpackArgs("neq", args, kwargs, "x", &x, "y", &y, "msg?", &msg); err != nil {
		return nil, err
	}
	if ok, err := starlark.Equal(x, y); err != nil {
		return nil, err
	} else if ok {
		if msg == "" {
			msg = fmt.Sprintf("%v == %v", x, y)
		}
		return nil, fail(thread, "must.neq failed: %s", msg)
	}
	return starlark.None, nil
}

func mustTrue(
	thread *starlark.Thread,
	b *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var cond starlark.Value
	var msg string
	if err := starlark.UnpackArgs("true", args, kwargs, "cond", &cond, "msg?", &msg); err != nil {
		return nil, err
	}
	if !cond.Truth() {
		if msg == "" {
			msg = "expected True, got False"
		}
		return nil, fail(thread, "must.true failed: %s", msg)
	}
	return starlark.None, nil
}

func mustFalse(
	thread *starlark.Thread,
	b *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var cond starlark.Value
	var msg string
	if err := starlark.UnpackArgs("false", args, kwargs, "cond", &cond, "msg?", &msg); err != nil {
		return nil, err
	}
	if cond.Truth() {
		if msg == "" {
			msg = "expected False, got True"
		}
		return nil, fail(thread, "must.false failed: %s", msg)
	}
	return starlark.None, nil
}

func mustFails(
	thread *starlark.Thread,
	b *starlark.Builtin,
	args starlark.Tuple,
	kwargs []starlark.Tuple,
) (starlark.Value, error) {
	var f starlark.Callable
	var msg string
	if err := starlark.UnpackArgs("fails", args, kwargs, "f", &f, "msg?", &msg); err != nil {
		return nil, err
	}
	_, err := starlark.Call(thread, f, nil, nil)
	if err == nil {
		if msg == "" {
			msg = "expected function to fail but it succeeded"
		}
		return nil, fail(thread, "must.fails failed: %s", msg)
	}
	return starlark.None, nil
}
