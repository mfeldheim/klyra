package action

import "fmt"

var factories = map[string]Factory{}

func Register(typ string, f Factory) {
	factories[typ] = f
}

func New(typ, name string, cfg map[string]any) (Action, error) {
	f, ok := factories[typ]
	if !ok {
		return nil, fmt.Errorf("unknown action type %q", typ)
	}
	return f(name, cfg)
}
