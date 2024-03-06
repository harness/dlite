package poller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/icrowley/fake"
	"github.com/wings-software/dlite/client"
	"github.com/wings-software/dlite/router"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/utils/strings/slices"
)

var (
	// Time period between sending heartbeats to the server
	hearbeatInterval  = 10 * time.Second
	heartbeatTimeout  = 15 * time.Second
	taskEventsTimeout = 30 * time.Second
)

type FilterFn func(*client.TaskEvent) bool

type Poller struct {
	AccountID     string
	AccountSecret string
	Name          string   // name of the runner
	Tags          []string // list of tags that the runner accepts
	Client        client.Client
	Router        router.Router
	Filter        FilterFn
	// The Harness manager allows two task acquire calls with the same delegate ID to go through (by design).
	// We need to make sure two different threads do not acquire the same task.
	// This map makes sure Acquire() is called only once per task ID. The mapping is removed once the status
	// for the task has been sent.
	m sync.Map
}

type DelegateInfo struct {
	Host string
	IP   string
	ID   string
	Name string
}

func New(accountID, accountSecret, name string, tags []string, c client.Client, r router.Router) *Poller {
	return &Poller{
		AccountID:     accountID,
		AccountSecret: accountSecret,
		Tags:          tags,
		Name:          name,
		Client:        c,
		Router:        r,
		m:             sync.Map{},
	}
}

func (p *Poller) SetFilter(filter FilterFn) {
	p.Filter = filter
}

// Register registers the runner with the server. The server generates a delegate ID
// which is returned to the client.
func (p *Poller) Register(ctx context.Context) (*DelegateInfo, error) {
	host, err := os.Hostname()
	if err != nil {
		return nil, errors.Wrap(err, "could not get host name")
	}
	host = "dlite-" + strings.ReplaceAll(host, " ", "-")
	ip := getOutboundIP()
	id, err := p.register(ctx, hearbeatInterval, ip, host)
	if err != nil {
		logrus.WithField("ip", ip).WithField("host", host).WithError(err).Error("could not register runner")
		return nil, err
	}
	return &DelegateInfo{
		ID:   id,
		Host: host,
		IP:   ip,
		Name: p.Name,
	}, nil
}

// Poll continually asks the task server for tasks to execute. It executes the tasks by routing
// them to the correct handler and updating the status of the task to the server.
// id is the delegate instance ID. It's generated by the server on registration.
func (p *Poller) Poll(ctx context.Context, n int, id string, interval time.Duration) error {
	var wg sync.WaitGroup
	events := make(chan *client.TaskEvent, n)
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
				taskEventsCtx, cancelFn := context.WithTimeout(ctx, taskEventsTimeout)
				tasks, err := p.Client.GetTaskEvents(taskEventsCtx, id)
				if err != nil {
					logrus.WithError(err).Errorf("could not query for task events")
				}
				cancelFn()

				// Search for a task event matching the filter
				for _, ev := range tasks.TaskEvents {
					if p.Filter == nil || p.Filter(ev) {
						logrus.WithField("task_id", ev.TaskID).Info("trying to acquire task")
						events <- ev
					}
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
					err := p.execute(ctx, id, *task, i)
					if err != nil {
						logrus.WithError(err).WithField("task_id", task.TaskID).Errorf("[Thread %d]: could not perform task execution", i)
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
func (p *Poller) execute(ctx context.Context, delegateID string, ev client.TaskEvent, i int) error {
	taskID := ev.TaskID
	if _, loaded := p.m.LoadOrStore(taskID, true); loaded {
		return nil
	}
	defer p.m.Delete(taskID)
	task, err := p.Client.Acquire(ctx, delegateID, taskID)
	if err != nil {
		logrus.WithError(err).WithField("task_id", taskID).Warnln("failed to acquire task")
		return nil
	}
	var buf bytes.Buffer
	err = json.NewEncoder(&buf).Encode(task)
	if err != nil {
		return errors.Wrap(err, "failed to encode task")
	}
	logrus.Infof("[Thread %d]: successfully acquired taskID: %s of type: %s", i, taskID, task.Type)
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
	err = p.Client.SendStatus(context.Background(), delegateID, taskID, taskResponse)
	if err != nil {
		return errors.Wrap(err, "failed to send step status")
	}
	logrus.Infof("[Thread %d]: successfully completed task execution of taskID: %s of type: %s", i, taskID, task.Type)
	return nil
}

// Register registers the runner and runs a background thread which keeps pinging the server
// at a period of interval. It returns the delegate ID.
func (p *Poller) register(ctx context.Context, interval time.Duration, ip, host string) (string, error) {
	req := &client.RegisterRequest{
		AccountID:          p.AccountID,
		DelegateName:       p.Name,
		LastHeartbeat:      time.Now().UnixMilli(),
		Token:              p.AccountSecret,
		NG:                 true,
		Type:               "DOCKER",
		SequenceNum:        1,
		Polling:            true,
		HostName:           host,
		IP:                 ip,
		SupportedTaskTypes: p.Router.Routes(),
		Tags:               p.Tags,
		HeartbeatAsObject:  true,
	}
	resp, err := p.Client.Register(ctx, req)
	if err != nil {
		return "", errors.Wrap(err, "could not register the runner")
	}
	req.ID = resp.Resource.DelegateID
	logrus.WithField("id", req.ID).WithField("host", req.HostName).
		WithField("ip", req.IP).Info("registered delegate successfully")
	p.heartbeat(ctx, req, interval)
	return resp.Resource.DelegateID, nil
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
				req.LastHeartbeat = time.Now().UnixMilli()
				heartbeatCtx, cancelFn := context.WithTimeout(ctx, heartbeatTimeout)
				err := p.Client.Heartbeat(heartbeatCtx, req)
				if err != nil {
					logrus.WithError(err).Errorf("could not send heartbeat")
				}
				cancelFn()
			}
		}
	}()
}

// Get preferred outbound ip of this machine. It returns a fake IP in case of errors.
func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		logrus.WithError(err).Error("could not figure out an IP, using a randomly generated IP")
		return "fake-" + fake.IPv4()
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}
