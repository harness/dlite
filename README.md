# About

A golang client which can interact and accept tasks from the Harness manager (or any task management system which implements the same interface)

# Usage

A way to use this client would be:

Define task implementations:
```
var RouteMap = map[string]task.Handler{
	"CI_DOCKER_INITIALIZE_TASK": &setup.DockerInitializeTask{},
	"CI_DOCKER_EXECUTE_TASK":    &step.DockerExecuteTask{},
	"CI_DOCKER_CLEANUP_TASK":    &cleanup.DockerCleanupTask{},
}
```

Define a task. A task needs to implement the HTTP handler interface:
```
// A task must implement the handler interface. Example:
type DockerInitializeTask struct {
}

// DockerInitializeTask initializes a pipeline execution
type DockerInitTaskRequest struct {}
type DockerInitTaskResponse struct {}
func (t *DockerInitializeTask) ServeHTTP(w http.ResponseWriter, req *http.Request) {
  task := &client.Task{}
  err := json.NewDecoder(r.Body).Decode(task)
  if err != nil {
    httphelper.WriteBadRequest(w, err)
    return
  }
  // Unmarshal the task data
  taskBytes, err := task.Data.MarshalJSON()
  if err != nil {
     httphelper.WriteBadRequest(w, err)
     return
  }
  d := &DockerInitTaskRequest{}
  err = json.Unmarshal(taskBytes, d)
  if err != nil {
    httphelper.WriteBadRequest(w, err)
    return
  }
  // Write the response to writer
  obj := DockerExecutionResponse{}
  httphelper.WriteJSON(w, obj, 200)
}
```

Register the routes:
```
// These routes can be registered with the router
router := router.NewRouter(router.RouteMap)
```

Create a client and start polling for tasks:
```
// Create a delegate client
client := delegate.Client(...)

// The poller needs a client that interacts with the task management system and a router to route the tasks
poller := poller.New(...)

// Start polling for tasks
err := poller.Poll(ctx, parallelExecutors, interval)
```

# Future goals

The goal is for this client to become the defacto interface of interacting with both the Harness manager as well as the Drone server for accepting and executing tasks. It should be pluggable into any of the existing drone runners and be used for both Harness CIE and Drone.
