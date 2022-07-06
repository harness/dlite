package task

import (
	"net/http"
)

// Task Handlers should implement the HTTP handler interface
// This interface can be extended later to support other communication mechanisms
type Handler interface {
	// http Handler implements ServeHTTP(ResponseWriter, *Request)
	// For Harness, the request contains a task definition in the body and the task response
	// should be written to the ResponseWriter
	http.Handler
}
