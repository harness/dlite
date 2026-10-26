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

// Register the poller
info, err := poller.Register(...)

// Start polling for tasks
err := poller.Poll(ctx, parallelExecutors, info.ID, ... ,interval)
```

# Future goals

The goal is for this client to become the defacto interface of interacting with both the Harness manager as well as the Drone server for accepting and executing tasks. It should be pluggable into any of the existing drone runners and be used for both Harness CIE and Drone.

# Release Procedure

## Security builds

dlite is a Go library; this repository publishes source tags and does not build
release binaries or runtime images. Its `toolchain` directive selects the preferred
compiler when working in this module, but does not select the compiler used by
applications that import it. The minimum Go version is 1.25 because the patched
`golang.org/x/sys` dependency requires it.

This source release raises the minimum from Go 1.24 to Go 1.25. Consumers using
an older compiler with toolchain downloads disabled must update their builder
before upgrading dlite. The `go` directive enforces this dependency requirement;
the preferred `toolchain` directive does not enforce a security-patched compiler
in a consuming application.

For the [eMudhra incident (GO-2026-6809)](https://pkg.go.dev/vuln/GO-2026-6809),
consumers such as lite-engine and drone-runner-aws must update to the fixed dlite
release and rebuild their binaries with Go 1.26.10 or a later patched release
(Go 1.27 requires at least 1.27.3, as listed in the
[official advisory's version ranges](https://vuln.go.dev/ID/GO-2026-6809.json)).
Their build and runtime images must also use
the updated trust store. This repository's CI uses the digest-pinned HHI Go image
and disables toolchain downloads with `GOTOOLCHAIN=local`.

After publishing the source release, rebuild, scan and publish each consuming
application's artifacts before updating centralized image versions. Passing this
library's CI does not verify already-published downstream binaries or images.

The downstream release checks remain open until evidence is linked below:

| Consumer | Fixed dlite source version | Patched compiler and trust store | Published artifact scan |
| --- | --- | --- | --- |
| [lite-engine](https://github.com/harness/lite-engine) | Pending source release and dependency update | Pending rebuild evidence | Pending |
| [drone-runner-aws](https://github.com/harness/drone-runner-aws) | Pending source release and dependency update | Pending rebuild evidence | Pending |

For each consumer, record the source version, builder and runtime image digests,
`go version -m` output for the published binary, and its vulnerability scan.
Do not mark the incident remediated for that consumer until these checks pass.
When a new Go security fix is published, update the CI image tag and digest
together and rerun tests, lint and govulncheck. Digest pins deliberately require
an explicit update; `GOTOOLCHAIN=local` makes unexpected toolchain requirements
fail instead of downloading an unreviewed compiler.

## Run the changelog generator

```BASH
docker run -it --rm -v "$(pwd)":/usr/local/src/your-app githubchangeloggenerator/github-changelog-generator -u harness -p dlite -t <secret github token>
```

You can generate a token by logging into your GitHub account and going to Settings -> Personal access tokens.

Next we tag the PR's with the fixes or enhancements labels. If the PR does not fufil the requirements, do not add a label.

Run the changelog generator again with the future version according to semver.

```BASH
docker run -it --rm -v "$(pwd)":/usr/local/src/your-app githubchangeloggenerator/github-changelog-generator -u drone-runners -p drone-runner-aws -t <secret token> --future-release v1.0.0
```

Create your pull request for the release. Get it merged then tag the release.

