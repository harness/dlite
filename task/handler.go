package task

import (
	"io"

	"github.com/wings-software/dlite/client"
)

// Handler is an interface for a task implementation
type Handler interface {
	// Handle implements a task and writes back the response
	Handle(*client.Task, io.Writer)
}
