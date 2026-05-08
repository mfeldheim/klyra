package monitor

import "fmt"

var factories = map[string]Factory{}

func Register(typ string, f Factory) {
	if _, exists := factories[typ]; exists {
		panic(fmt.Sprintf("monitor type %q already registered", typ))
	}
	factories[typ] = f
}

func New(typ, name string, cfg map[string]any) (Monitor, error) {
	f, ok := factories[typ]
	if !ok {
		return nil, fmt.Errorf("unknown monitor type %q", typ)
	}
	return f(name, cfg)
}
