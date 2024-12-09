package delegate

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/sirupsen/logrus"
	"github.com/wings-software/dlite/client"

	"github.com/wings-software/dlite/logger"
)

const (
	registerEndpoint         = "/api/agent/delegates/register?accountId=%s"
	heartbeatEndpoint        = "/api/agent/delegates/heartbeat-with-polling?accountId=%s"
	taskPollEndpoint         = "/api/agent/delegates/%s/task-events?accountId=%s"
	taskAcquireEndpoint      = "/api/agent/v2/delegates/%s/tasks/%s/acquire?accountId=%s&delegateInstanceId=%s"
	taskStatusEndpoint       = "/api/agent/v2/tasks/%s/delegates/%s?accountId=%s"
	taskStatusEndpointV2     = "/api/executions/%s/task-response?runnerId=%s&accountId=%s"
	runnerTaskStatusEndpoint = "/api/executions/%s/response?accountId=%s&delegateId=%s"
	delegateCapacityEndpoint = "/api/agent/delegates/register-delegate-capacity/%s?accountId=%s"
)

var (
	registerTimeout      = 30 * time.Second
	taskEventsTimeout    = 60 * time.Second
	sendStatusRetryTimes = 5
)

// defaultClient is the default http.Client.
var defaultClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// New returns a new client.
func New(endpoint, id, secret string, skipverify bool, additionalCertsDir string) *HTTPClient {
	return getClient(endpoint, id, "", NewTokenCache(id, secret), skipverify, additionalCertsDir)
}

func NewFromToken(endpoint, id, token string, skipverify bool, additionalCertsDir string) *HTTPClient {
	return getClient(endpoint, id, token, nil, skipverify, additionalCertsDir)
}

func getClient(endpoint, id, token string, cache *TokenCache, skipverify bool, additionalCertsDir string) *HTTPClient {
	log := logrus.New()
	httpClient := &HTTPClient{
		Logger:            log,
		Endpoint:          endpoint,
		SkipVerify:        skipverify,
		AccountID:         id,
		Client:            defaultClient,
		AccountTokenCache: cache,
		Token:             token,
	}

	// Load mTLS certificates if available
	mtlsEnabled, mtlsCerts := loadMTLSCerts(log, "/etc/mtls/client.crt", "/etc/mtls/client.key")

	// Load custom root CAs if additional certificates directory is provided
	rootCAs := loadRootCAs(log, additionalCertsDir)

	// Only create HTTP client if needed (mTLS, additional certs, or skipverify)
	if skipverify || rootCAs != nil || mtlsEnabled {
		httpClient.Client = clientWithTLSConfig(skipverify, rootCAs, mtlsEnabled, &mtlsCerts)
	}

	return httpClient
}

func loadRootCAs(log *logrus.Logger, additionalCertsDir string) *x509.CertPool {
	if additionalCertsDir == "" {
		return nil
	}

	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}

	log.Infof("additional certs dir to allow: %s\n", additionalCertsDir)

	files, err := os.ReadDir(additionalCertsDir)
	if err != nil {
		log.Errorf("could not read directory %s, error: %s", additionalCertsDir, err)
		return rootCAs
	}

	// Go through all certs in this directory and add them to the global certs
	for _, f := range files {
		path := filepath.Join(additionalCertsDir, f.Name())
		log.Infof("trying to add certs at: %s to root certs\n", path)
		// Create TLS config using cert PEM
		rootPem, err := os.ReadFile(path)
		if err != nil {
			log.Errorf("could not read certificate file (%s), error: %s", path, err.Error())
			continue
		}
		// Append certs to the global certs
		ok := rootCAs.AppendCertsFromPEM(rootPem)
		if !ok {
			log.Errorf("error adding cert (%s) to pool, please check format of the certs provided", path)
			continue
		}
		log.Infof("successfully added cert at: %s to root certs", path)
	}
	return rootCAs
}

// loadMTLSCerts loads mTLS certificates if they exist
func loadMTLSCerts(log *logrus.Logger, certFile, keyFile string) (bool, tls.Certificate) {
	if fileExists(certFile) && fileExists(keyFile) {
		mtlsCerts, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			log.Errorf("failed to load mTLS cert/key pair, error: %s\n", err)
			return false, tls.Certificate{}
		}
		return true, mtlsCerts
	}
	return false, tls.Certificate{}
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	return err == nil && !info.IsDir()
}

func clientWithTLSConfig(skipverify bool, rootCAs *x509.CertPool, mtlsEnabled bool, cert *tls.Certificate) *http.Client {
	// Create the HTTP Client with certs
	config := &tls.Config{
		//nolint:gosec
		InsecureSkipVerify: skipverify,
	}
	if !skipverify && rootCAs != nil {
		config.RootCAs = rootCAs
	}
	if mtlsEnabled {
		config.Certificates = []tls.Certificate{*cert}
	}
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			TLSClientConfig: config,
		},
	}
}

// An HTTPClient manages communication with the runner API.
type HTTPClient struct {
	Client            *http.Client
	Logger            logger.Logger
	Endpoint          string
	AccountID         string
	AccountTokenCache *TokenCache
	SkipVerify        bool
	Token             string
}

// Register registers the runner with the manager
func (p *HTTPClient) Register(ctx context.Context, r *client.RegisterRequest) (*client.RegisterResponse, error) {
	req := r
	resp := &client.RegisterResponse{}
	path := fmt.Sprintf(registerEndpoint, p.AccountID)
	_, err := p.retry(ctx, path, "POST", req, resp, createBackoff(ctx, registerTimeout), true) //nolint: bodyclose
	return resp, err
}

// Heartbeat sends a periodic heartbeat to the server
func (p *HTTPClient) Heartbeat(ctx context.Context, r *client.RegisterRequest) error {
	req := r
	path := fmt.Sprintf(heartbeatEndpoint, p.AccountID)
	_, err := p.do(ctx, path, "POST", req, nil)
	return err
}

// RegisterCapacity registers maximum number of CI Stages that can run on the host
func (p *HTTPClient) RegisterCapacity(ctx context.Context, delID string, r *client.DelegateCapacity) error {
	req := r
	path := fmt.Sprintf(delegateCapacityEndpoint, delID, p.AccountID)
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
	retryNumber := 0
	var err error
	for retryNumber < sendStatusRetryTimes {
		_, err = p.retry(ctx, path, "POST", req, nil, createBackoff(ctx, taskEventsTimeout), true) //nolint: bodyclose
		if err == nil {
			return nil
		}
		retryNumber++
	}
	return err
}

// SendStatusV2 updates the status of a task submitted via Harness Runner (V2 Task Status endpoint)
func (p *HTTPClient) SendStatusV2(ctx context.Context, runnerID, taskID string, r *client.RunnerTaskResponse) error {
	path := fmt.Sprintf(taskStatusEndpointV2, taskID, runnerID, p.AccountID)
	req := r
	retryNumber := 0
	var err error
	for retryNumber < sendStatusRetryTimes {
		_, err = p.retry(ctx, path, "POST", req, nil, createBackoff(ctx, taskEventsTimeout), true) //nolint: bodyclose
		if err == nil {
			return nil
		}
		retryNumber++
	}
	return err
}

func (p *HTTPClient) SendRunnerStatus(ctx context.Context, delegateID, taskID string, r *client.RunnerTaskResponse) error {
	path := fmt.Sprintf(runnerTaskStatusEndpoint, taskID, p.AccountID, delegateID)
	req := r
	retryNumber := 0
	var err error
	for retryNumber < sendStatusRetryTimes {
		_, err = p.retry(ctx, path, "POST", req, nil, createBackoff(ctx, taskEventsTimeout), true) //nolint: bodyclose
		if err == nil {
			return nil
		}
		retryNumber++
	}
	return err
}

func (p *HTTPClient) retry(ctx context.Context, path, method string, in, out interface{}, b backoff.BackOffContext, ignoreStatusCode bool) (*http.Response, error) { //nolint: unparam
	for {
		res, err := p.do(ctx, path, method, in, out)
		// do not retry on Canceled or DeadlineExceeded
		if ctxErr := ctx.Err(); ctxErr != nil {
			p.logger().Errorf("http: context canceled")
			return res, ctxErr
		}

		duration := b.NextBackOff()

		if res != nil {
			// Check the response code. We retry on 500-range
			// responses to allow the server time to recover, as
			// 500's are typically not permanent errors and may
			// relate to outages on the server side.
			if (ignoreStatusCode && err != nil) || res.StatusCode > 501 {
				p.logger().Errorf("http: server error: re-connect and re-try: %s", err)
				if duration == backoff.Stop {
					p.logger().Errorf("max retry limit reached, task status won't be updated")
					return nil, err
				}
				time.Sleep(duration)
				continue
			}
		} else if err != nil {
			p.logger().Errorf("http: request error: %s", err)
			if duration == backoff.Stop {
				p.logger().Errorf("max retry limit reached, task status won't be updated")
				return nil, err
			}
			time.Sleep(duration)
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
		if err := json.NewEncoder(&buf).Encode(in); err != nil {
			p.logger().Errorf("could not encode input payload: %s", err)
		}
	}

	endpoint := p.Endpoint + path
	req, err := http.NewRequest(method, endpoint, &buf)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)

	// the request should include the secret shared between
	// the agent and server for authorization.
	token := ""
	if p.Token != "" {
		token = p.Token
	} else {
		token, err = p.AccountTokenCache.Get()
		if err != nil {
			p.logger().Errorf("could not generate account token: %s", err)
			return nil, err
		}
	}
	req.Header.Add("Authorization", "Delegate "+token)
	req.Header.Add("Content-Type", "application/json")
	res, err := p.Client.Do(req)
	if res != nil {
		defer func() {
			// drain the response body so we can reuse
			// this connection.
			if _, err = io.Copy(io.Discard, io.LimitReader(res.Body, 4096)); err != nil {
				p.logger().Errorf("could not drain response body: %s", err)
			}
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
	body, err := io.ReadAll(res.Body)
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

// logger is a helper function that returns the default logger
// if a custom logger is not defined.
func (p *HTTPClient) logger() logger.Logger {
	if p.Logger == nil {
		return logger.Discard()
	}
	return p.Logger
}

func createBackoff(ctx context.Context, maxElapsedTime time.Duration) backoff.BackOffContext {
	exp := backoff.NewExponentialBackOff()
	exp.MaxElapsedTime = maxElapsedTime
	return backoff.WithContext(exp, ctx)
}
