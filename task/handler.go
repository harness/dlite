package task

import (
	"dlite/client"
	"io"
)

// Handler is an interface for a task implementation
type Handler interface {
	// Handle implements a task and writes back the response
	Handle(*client.Task, io.Writer)
}
