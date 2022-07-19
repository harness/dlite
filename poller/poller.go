package poller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/wings-software/dlite/client"
	"github.com/wings-software/dlite/router"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/utils/strings/slices"
)

var (
	// Time period between sending heartbeats to the server
	hearbeatInterval = 10 * time.Second
)

type poller struct {
	AccountID     string
	AccountSecret string
	Name          string   // name of the runner
	Tags          []string // list of tags that the runner accepts
	Client        client.Client
	Router        router.Router
}

func New(accountID, accountSecret, name string, tags []string, client client.Client, router router.Router) *poller {
	return &poller{
		AccountID:     accountID,
		AccountSecret: accountSecret,
		Tags:          tags,
		Name:          name,
		Client:        client,
		Router:        router,
	}
}

// Poll continually asks the task server for tasks to execute. It executes the tasks by routing
// them to the correct handler and updating the status of the task to the server.
func (p *poller) Poll(ctx context.Context, n int, interval time.Duration) error {
	err := p.register(ctx, hearbeatInterval)
	if err != nil {
		return fmt.Errorf("could not register the delegate: %w", err)
	}
	var wg sync.WaitGroup
	events := make(chan client.TaskEvent, n)
	// Task event poller
	go func() {
		pollTimer := time.NewTimer(interval)
		for {
			pollTimer.Reset(interval)
			select {
			case <-ctx.Done():
				logrus.Error("context canceled")
				return
			case <-pollTimer.C:
				tasks, err := p.Client.GetTaskEvents(ctx, p.Name)
				if err != nil {
					logrus.Errorf("could not query for task events")
				}
				if len(tasks.TaskEvents) > 0 {
					events <- tasks.TaskEvents[0]
				}
			}
		}
	}()
	// Task event executor
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			for {
				select {
				case <-ctx.Done():
					wg.Done()
					return
				case task := <-events:
					err := p.execute(ctx, task, i)
					if err != nil {
						logrus.Errorf("[Thread %d]: could not dispatch task with error: %s", i, err)
					}
				}
			}
		}(i)
	}
	logrus.Infof("initialized %d threads successfully and starting polling for tasks", n)
	wg.Wait()
	return nil
}

// execute tries to acquire the task and executes the handler for it
func (p *poller) execute(ctx context.Context, ev client.TaskEvent, i int) error {
	id := ev.TaskID
	task, err := p.Client.Acquire(ctx, p.Name, id)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	err = json.NewEncoder(&buf).Encode(task)
	if err != nil {
		return err
	}
	logrus.Infof("[Thread %d]: successfully acquired taskID: %s of type: %s", i, id, task.Type)
	if !slices.Contains(p.Router.Routes(), task.Type) { // should not happen
		logrus.Errorf("[Thread %d]: Task ID of type: %s was never meant to reach this delegate", i, task.Type)
		return fmt.Errorf("task type not supported by delegate")
	}

	// TODO: Discuss possible better ways to forward the HTTP response to the task for processing
	// For now, keeping the handler interface consistent with the HTTP handler to allow for possible
	// extension in the future with CGI, etc.
	req, err := http.NewRequestWithContext(ctx, "POST", "/", &buf)
	if err != nil {
		return err
	}

	writer := NewResponseWriter()
	p.Router.Route(task.Type).ServeHTTP(writer, req)
	taskResponse := &client.TaskResponse{
		ID:   task.ID,
		Data: writer.buf.Bytes(),
		Code: "OK",
		Type: task.Type,
	}
	err = p.Client.SendStatus(ctx, p.Name, id, taskResponse)
	if err != nil {
		return err
	}
	logrus.Infof("[Thread %d]: successfully completed task execution of taskID: %s of type: %s", i, id, task.Type)
	return nil
}

// Register registers the runner and runs a background thread which keeps pinging the server
// at a period of interval.
func (p *poller) register(ctx context.Context, interval time.Duration) error {
	host, err := os.Hostname()
	if err != nil {
		return errors.Wrap(err, "could not get host name")
	}
	req := &client.RegisterRequest{
		AccountID:          p.AccountID,
		DelegateName:       p.Name,
		Token:              p.AccountSecret,
		ID:                 p.Name,
		NG:                 true,
		Type:               "DOCKER",
		SequenceNum:        1,
		Polling:            true,
		HostName:           host,
		IP:                 p.Name, // TODO: We should change this to actual IP but that was creating issues with restarts
		SupportedTaskTypes: p.Router.Routes(),
		Tags:               p.Tags,
	}
	err = p.Client.Register(ctx, req)
	if err != nil {
		return errors.Wrap(err, "could not register the runner")
	}
	logrus.Infof("registered delegate successfully")
	p.heartbeat(ctx, req, interval)
	return nil
}

// heartbeat starts a periodic thread in the background which continually pings the server
func (p *poller) heartbeat(ctx context.Context, req *client.RegisterRequest, interval time.Duration) {
	go func() {
		msgDelayTimer := time.NewTimer(interval)
		defer msgDelayTimer.Stop()
		for {
			msgDelayTimer.Reset(interval)
			select {
			case <-ctx.Done():
				logrus.Error("context canceled")
				return
			case <-msgDelayTimer.C:
				err := p.Client.Heartbeat(ctx, req)
				if err != nil {
					logrus.Errorf("could not send heartbeat with error: %s", err)
				}
			}
		}
	}()
}
