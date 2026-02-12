package main

import "encoding/json"

type ToolHandler func(args json.RawMessage) (string, error)

type ToolRegistry struct {
	defs     []ToolDef
	handlers map[string]ToolHandler
	// Tools that are blocked in plan mode
	writeTools map[string]bool
	// Tools denied by config
	deniedTools map[string]bool
}

func NewToolRegistry(toolsCfg ToolsConfig) *ToolRegistry {
	r := &ToolRegistry{
		handlers:    make(map[string]ToolHandler),
		writeTools:  make(map[string]bool),
		deniedTools: make(map[string]bool),
	}
	r.registerAll()
	for _, name := range toolsCfg.Deny {
		r.deniedTools[name] = true
	}
	// If allow list is set, deny everything not in it
	if len(toolsCfg.Allow) > 0 {
		allowed := make(map[string]bool)
		for _, name := range toolsCfg.Allow {
			allowed[name] = true
		}
		for _, def := range r.defs {
			if !allowed[def.Name] {
				r.deniedTools[def.Name] = true
			}
		}
	}
	return r
}

func (r *ToolRegistry) Register(def ToolDef, handler ToolHandler, isWrite bool) {
	r.defs = append(r.defs, def)
	r.handlers[def.Name] = handler
	if isWrite {
		r.writeTools[def.Name] = true
	}
}

func (r *ToolRegistry) Definitions() []ToolDef {
	if len(r.deniedTools) == 0 {
		return r.defs
	}
	var filtered []ToolDef
	for _, def := range r.defs {
		if !r.deniedTools[def.Name] {
			filtered = append(filtered, def)
		}
	}
	return filtered
}

func (r *ToolRegistry) Execute(name string, args json.RawMessage, mode Mode) (string, error) {
	if r.deniedTools[name] {
		return "blocked: tool denied by config", nil
	}
	if mode == ModePlan && r.writeTools[name] {
		return "blocked: not allowed in plan mode", nil
	}

	handler, ok := r.handlers[name]
	if !ok {
		return "", nil
	}
	return handler(args)
}

func (r *ToolRegistry) IsWriteTool(name string) bool {
	return r.writeTools[name]
}

func (r *ToolRegistry) registerAll() {
	registerFSTools(r)
	registerExecTools(r)
	registerSearchTools(r)
	registerDiffTools(r)
	registerUserTools(r)
}
