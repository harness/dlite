package poller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/wings-software/dlite/client"
	"github.com/wings-software/dlite/router"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/utils/strings/slices"
)

type Poller struct {
	AccountID     string
	AccountSecret string
	Name          string // name of the runner
	Client        client.Client
	Router        router.Router
}

func New(accountID, accountSecret, name string, client client.Client, router router.Router) *Poller {
	return &Poller{
		AccountID:     accountID,
		AccountSecret: accountSecret,
		Name:          name,
		Client:        client,
		Router:        router,
	}
}

// Register registers the runner and runs a background thread which keeps pinging the server
// at a period of interval.
func (p *Poller) Register(ctx context.Context, tags []string, interval time.Duration) error {
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
		Tags:               tags,
	}
	err = p.Client.Register(ctx, req)
	if err != nil {
		return errors.Wrap(err, "could not register the runner")
	}
	logrus.Infof("registered delegate successfully")
	p.heartbeat(ctx, req, interval)
	return nil
}

// Poll continually asks the task server for tasks to execute. It executes the tasks by routing
// them to the correct handler and updating the status of the task to the server.
func (p *Poller) Poll(ctx context.Context, n int, interval time.Duration) {
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
			logrus.Infof("initialized thread %d", i)
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
	wg.Wait()
}

// execute tries to acquire the task and executes the handler for it
func (p *Poller) execute(ctx context.Context, ev client.TaskEvent, i int) error {
	id := ev.TaskID
	task, err := p.Client.Acquire(ctx, p.Name, id)
	if err != nil {
		return err
	}
	g, _ := json.Marshal(task)
	fmt.Println("acquired task is: ", string(g))
	if task.Type == "" {
		return nil
	}
	logrus.Infof("[Thread %d]: successfully acquired taskID: %s of type: %s", i, id, task.Type)
	if !slices.Contains(p.Router.Routes(), task.Type) {
		logrus.Errorf("[Thread %d]: Task ID of type: %s was never meant to reach this delegate", i, task.Type)
		return nil
	}
	var buf bytes.Buffer
	p.Router.Route(task.Type).Handle(task, &buf)
	resp := &client.TaskResponse{
		ID:   task.ID,
		Data: buf.Bytes(),
		Code: "OK",
		Type: task.Type,
	}
	ga, _ := json.Marshal(resp)
	fmt.Println("sending response back: ", string(ga))
	err = p.Client.SendStatus(ctx, p.Name, id, resp)
	if err != nil {
		return err
	}
	logrus.Infof("[Thread %d]: successfully completed task execution of taskID: %s of type: %s", i, id, task.Type)
	return nil
}

// heartbeat starts a periodic thread in the background which continually pings the server
func (p *Poller) heartbeat(ctx context.Context, req *client.RegisterRequest, interval time.Duration) {
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
