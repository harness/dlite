package delegate

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/wings-software/dlite/client"

	"github.com/wings-software/dlite/logger"
)

const (
	registerEndpoint    = "/api/agent/delegates/register?accountId=%s"
	heartbeatEndpoint   = "/api/agent/delegates/heartbeat-with-polling?accountId=%s"
	taskPollEndpoint    = "/api/agent/delegates/%s/task-events?accountId=%s"
	taskAcquireEndpoint = "/api/agent/v2/delegates/%s/tasks/%s/acquire?accountId=%s&delegateInstanceId=%s"
	taskStatusEndpoint  = "/api/agent/v2/tasks/%s/delegates/%s?accountId=%s"
)

// defaultClient is the default http.Client.
var defaultClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// New returns a new client.
func New(endpoint, accountID, token string, skipverify bool) *HTTPClient {
	log := logger.Logrus(nil)
	client := &HTTPClient{
		Logger:     log,
		Endpoint:   endpoint,
		SkipVerify: skipverify,
		AccountID:  accountID,
		Token:      token,
	}
	if skipverify {
		client.Client = &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		}
	}
	return client
}

// An HTTPClient manages communication with the runner API.
type HTTPClient struct {
	Client     *http.Client
	Logger     logger.Logger
	Endpoint   string
	Token      string
	AccountID  string
	SkipVerify bool
}

// Register registers the runner with the manager
func (p *HTTPClient) Register(ctx context.Context, r *client.RegisterRequest) error {
	req := r
	path := fmt.Sprintf(registerEndpoint, p.AccountID)
	_, err := p.retry(ctx, path, "POST", req, nil)
	return err
}

// Heartbeat sends a periodic heartbeat to the server
func (p *HTTPClient) Heartbeat(ctx context.Context, r *client.RegisterRequest) error {
	req := r
	path := fmt.Sprintf(heartbeatEndpoint, p.AccountID)
	_, err := p.do(ctx, path, "POST", req, nil)
	return err
}

// GetTaskEvents gets a list of events which can be executed on this runner
func (p *HTTPClient) GetTaskEvents(ctx context.Context, id string) (*client.TaskEventsResponse, error) {
	path := fmt.Sprintf(taskPollEndpoint, id, p.AccountID)
	events := &client.TaskEventsResponse{}
	_, err := p.do(ctx, path, "GET", nil, events)
	return events, err
}

// Acquire tries to acquire a specific task
func (p *HTTPClient) Acquire(ctx context.Context, delegateID, taskID string) (*client.Task, error) {
	path := fmt.Sprintf(taskAcquireEndpoint, delegateID, taskID, p.AccountID, delegateID)
	task := &client.Task{}
	_, err := p.do(ctx, path, "PUT", nil, task)
	return task, err
}

// SendStatus updates the status of a task
func (p *HTTPClient) SendStatus(ctx context.Context, delegateID, taskID string, r *client.TaskResponse) error {
	path := fmt.Sprintf(taskStatusEndpoint, taskID, delegateID, p.AccountID)
	req := r
	_, err := p.retry(ctx, path, "POST", req, nil)
	return err
}

func (p *HTTPClient) retry(ctx context.Context, path, method string, in, out interface{}) (*http.Response, error) {
	for {
		res, err := p.do(ctx, path, method, in, out)
		// do not retry on Canceled or DeadlineExceeded
		if err := ctx.Err(); err != nil {
			p.logger().Tracef("http: context canceled")
			return res, err
		}

		if res != nil {
			// Check the response code. We retry on 500-range
			// responses to allow the server time to recover, as
			// 500's are typically not permanent errors and may
			// relate to outages on the server side.
			if res.StatusCode > 501 {
				p.logger().Tracef("http: server error: re-connect and re-try: %s", err)
				time.Sleep(time.Second * 10)
				continue
			}
			// We also retry on 204 no content response codes,
			// used by the server when a long-polling request
			// is intentionally disconnected and should be
			// automatically reconnected.
			if res.StatusCode == 204 {
				p.logger().Tracef("http: no content returned: re-connect and re-try")
				time.Sleep(time.Second * 10)
				continue
			}
		} else if err != nil {
			p.logger().Tracef("http: request error: %s", err)
			time.Sleep(time.Second * 10)
			continue
		}
		return res, err
	}
}

// do is a helper function that posts a signed http request with
// the input encoded and response decoded from json.
func (p *HTTPClient) do(ctx context.Context, path, method string, in, out interface{}) (*http.Response, error) {
	var buf bytes.Buffer

	// marshal the input payload into json format and copy
	// to an io.ReadCloser.
	if in != nil {
		json.NewEncoder(&buf).Encode(in)
	}

	endpoint := p.Endpoint + path
	req, err := http.NewRequest(method, endpoint, &buf)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)

	// the request should include the secret shared between
	// the agent and server for authorization.
	req.Header.Add("Authorization", "Delegate "+p.Token)
	req.Header.Add("Content-Type", "application/json")
	res, err := p.client().Do(req)
	if res != nil {
		defer func() {
			// drain the response body so we can reuse
			// this connection.
			io.Copy(ioutil.Discard, io.LimitReader(res.Body, 4096))
			res.Body.Close()
		}()
	}
	if err != nil {
		return res, err
	}

	// if the response body return no content we exit
	// immediately. We do not read or unmarshal the response
	// and we do not return an error.
	if res.StatusCode == 204 {
		return res, nil
	}

	// else read the response body into a byte slice.
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return res, err
	}

	if res.StatusCode > 299 {
		// if the response body includes an error message
		// we should return the error string.
		if len(body) != 0 {
			return res, errors.New(
				string(body),
			)
		}
		// if the response body is empty we should return
		// the default status code text.
		return res, errors.New(
			http.StatusText(res.StatusCode),
		)
	}
	if out == nil {
		return res, nil
	}
	return res, json.Unmarshal(body, out)
}

// client is a helper funciton that returns the default client
// if a custom client is not defined.
func (p *HTTPClient) client() *http.Client {
	if p.Client == nil {
		return defaultClient
	}
	return p.Client
}

// logger is a helper funciton that returns the default logger
// if a custom logger is not defined.
func (p *HTTPClient) logger() logger.Logger {
	if p.Logger == nil {
		return logger.Discard()
	}
	return p.Logger
}
