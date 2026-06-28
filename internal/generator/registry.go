package generator

import (
	"context"
	"fmt"
	"sort"
)

type Context struct {
	Root string
	Lock LockFile
}

type GenerateMode int

const (
	ModeWrite GenerateMode = iota
	ModeVerify
)

type Suite interface {
	Name() string
	Fetch(context.Context, Context, SuiteLock) error
	Generate(context.Context, Context, SuiteLock, GenerateMode) error
}

type Registry struct {
	byName map[string]Suite
	names  []string
}

func NewRegistry(suites ...Suite) *Registry {
	registry := &Registry{
		byName: make(map[string]Suite),
	}
	for _, suite := range suites {
		name := suite.Name()
		registry.byName[name] = suite
		registry.names = append(registry.names, name)
	}
	sort.Strings(registry.names)
	return registry
}

func (r *Registry) Names() []string {
	names := make([]string, len(r.names))
	copy(names, r.names)
	return names
}

func (r *Registry) Select(names []string) ([]Suite, error) {
	if len(names) == 0 || (len(names) == 1 && names[0] == "all") {
		selected := make([]Suite, 0, len(r.names))
		for _, name := range r.names {
			selected = append(selected, r.byName[name])
		}
		return selected, nil
	}

	selected := make([]Suite, 0, len(names))
	for _, name := range names {
		suite, ok := r.byName[name]
		if !ok {
			return nil, fmt.Errorf("unknown suite %q", name)
		}
		selected = append(selected, suite)
	}
	return selected, nil
}
