package pihole

import (
	"fmt"
	"strings"
)

// InstanceConfig describes one Pi-hole instance for the registry.
type InstanceConfig struct {
	Name     string
	URL      string
	Password string
}

// Registry holds one or more named Pi-hole clients. The first instance in
// declaration order is the default, used when a tool request omits the
// "instance" argument and for resources (which always target the default).
type Registry struct {
	clients map[string]*Client
	order   []string
}

// NewRegistry builds a client per instance, applying the shared options (e.g.
// request timeout) to each. Declaration order is preserved; instances[0] is the
// default. The caller is responsible for passing a non-empty, name-unique list
// — config.Load guarantees both.
func NewRegistry(instances []InstanceConfig, opts ...Option) *Registry {
	r := &Registry{
		clients: make(map[string]*Client, len(instances)),
		order:   make([]string, 0, len(instances)),
	}
	for _, ic := range instances {
		clientOpts := append([]Option{WithName(ic.Name)}, opts...)
		r.clients[ic.Name] = New(ic.URL, ic.Password, clientOpts...)
		r.order = append(r.order, ic.Name)
	}
	return r
}

// SingleRegistry wraps an already-constructed client as a one-instance
// registry named after the client (or "primary" when unnamed). Useful for the
// single-instance path and for tests that build a client directly.
func SingleRegistry(c *Client) *Registry {
	if c.name == "" {
		c.name = "primary"
	}
	return &Registry{
		clients: map[string]*Client{c.name: c},
		order:   []string{c.name},
	}
}

// Default returns the first-declared instance's client.
func (r *Registry) Default() *Client {
	return r.clients[r.order[0]]
}

// Get returns the client for the named instance, or an error listing the valid
// names when the name is unknown.
func (r *Registry) Get(name string) (*Client, error) {
	if c, ok := r.clients[name]; ok {
		return c, nil
	}
	return nil, fmt.Errorf("unknown instance %q; configured instances: %s", name, strings.Join(r.order, ", "))
}

// All returns every client in declaration order.
func (r *Registry) All() []*Client {
	out := make([]*Client, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.clients[name])
	}
	return out
}

// Names returns the configured instance names in declaration order.
func (r *Registry) Names() []string {
	return append([]string(nil), r.order...)
}

// Len reports the number of configured instances.
func (r *Registry) Len() int {
	return len(r.order)
}

// Close releases every client's session.
func (r *Registry) Close() {
	for _, c := range r.clients {
		c.Close()
	}
}
