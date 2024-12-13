package pluginkit

import "sync"

// Registry maintains plugins and their stores/runners
type Registry struct {
	mu       sync.RWMutex
	plugins  map[string]*Plugin
	stores   map[string]Store
	runtimes map[string]Runtime
}

func NewRegistry() *Registry {
	return &Registry{
		plugins:  make(map[string]*Plugin),
		stores:   make(map[string]Store),
		runtimes: make(map[string]Runtime),
	}
}

func (r *Registry) RegisterStore(name string, store Store) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stores[name] = store
}

func (r *Registry) RegisterRuntime(name string, runtime Runtime) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runtimes[name] = runtime
}

// GetStore returns a store by name
func (r *Registry) GetStore(name string) (Store, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	store, ok := r.stores[name]

	return store, ok
}

// GetRuntime returns a runtime by name
func (r *Registry) GetRuntime(name string) (Runtime, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	runtime, ok := r.runtimes[name]

	return runtime, ok
}
