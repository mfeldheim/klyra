package action

import "fmt"

var factories = map[string]Factory{}

func Register(typ string, f Factory) {
	if _, exists := factories[typ]; exists {
		panic(fmt.Sprintf("action type %q already registered", typ))
	}
	factories[typ] = f
}

func New(typ, name string, cfg map[string]any) (Action, error) {
	f, ok := factories[typ]
	if !ok {
		return nil, fmt.Errorf("unknown action type %q", typ)
	}
	return f(name, cfg)
}
