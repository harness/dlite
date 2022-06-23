package router

import (
	"dlite/task"
)

type Router interface {
	// Routes returns a list of routes which have been defined in the router
	Routes() []string

	// Route returns a handler corresponding to a task type
	Route(string) task.Handler
}

// Router stores route mappings from task types to their handlers
type router struct {
	routes map[string]task.Handler
}

// NewRouter returns a new instance of a router
func NewRouter(routes map[string]task.Handler) *router {
	return &router{routes: routes}
}

// Route routes the incoming call to the appropriate handler
func (r *router) Route(taskType string) task.Handler {
	return r.routes[taskType]
}

// Routes returns all the supported task types by this runner version
func (r *router) Routes() []string {
	var routes []string
	for k := range r.routes {
		routes = append(routes, k)
	}
	return routes
}
