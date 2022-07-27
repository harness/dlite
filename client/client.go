package client

import (
	"context"
	"encoding/json"
)

// TODO: Make the structs more generic and remove Harness specific stuff
type (
	// Taken from existing manager API
	RegisterRequest struct {
		AccountID          string   `json:"accountId"`
		DelegateName       string   `json:"delegateName"`
		Token              string   `json:"delegateRandomToken"`
		ID                 string   `json:"delegateId"`
		Type               string   `json:"delegateType"`
		NG                 bool     `json:"ng"`
		Polling            bool     `json:"pollingModeEnabled"`
		HostName           string   `json:"hostName"`
		Connected          bool     `json:"connected"`
		KeepAlivePacket    bool     `json:"keepAlivePacket"`
		SequenceNum        int      `json:"sequenceNum"`
		IP                 string   `json:"ip"`
		SupportedTaskTypes []string `json:"supportedTaskTypes"`
		Tags               []string `json:"tags"`
	}

	RegisterResponse struct {
		DelegateID string `json:"delegateId"`
	}

	TaskEventsResponse struct {
		TaskEvents []TaskEvent `json:"delegateTaskEvents"`
	}

	TaskEvent struct {
		AccountID string `json:"accountId"`
		TaskID    string `json:"delegateTaskId"`
		Sync      bool   `json:"sync"`
	}

	Task struct {
		ID           string          `json:"id"`
		Type         string          `json:"type"`
		Data         json.RawMessage `json:"data"`
		Async        bool            `json:"async"`
		Timeout      int             `json:"timeout"`
		Logging      LogInfo         `json:"logging"`
		DelegateInfo DelegateInfo    `json:"delegate"`
		Capabilities json.RawMessage `json:"capabilities"`
	}

	LogInfo struct {
		Token        string            `json:"token"`
		Abstractions map[string]string `json:"abstractions"`
	}

	DelegateInfo struct {
		ID         string `json:"id"`
		InstanceID string `json:"instance_id"`
		Token      string `json:"token"`
	}

	TaskResponse struct {
		ID   string          `json:"id"`
		Data json.RawMessage `json:"data"`
		Type string          `json:"type"`
		Code string          `json:"code"` // OK, FAILED, RETRY_ON_OTHER_DELEGATE
	}
)

// Client is an interface which defines methods on interacting with a task managing system.
type Client interface {
	// Register registers the runner with the task server
	Register(ctx context.Context, r *RegisterRequest) (*RegisterResponse, error)

	// Heartbeat pings the task server to let it know that the runner is still alive
	Heartbeat(ctx context.Context, r *RegisterRequest) error

	// GetTaskEvents gets a list of pending tasks that need to be executed for this runner
	GetTaskEvents(ctx context.Context, name string) (*TaskEventsResponse, error)

	// Acquire tells the task server that the runner is ready to execute a task ID
	Acquire(ctx context.Context, name, taskID string) (*Task, error)

	// SendStatus sends a response to the task server for a task ID
	SendStatus(ctx context.Context, name, taskID string, req *TaskResponse) error
}
