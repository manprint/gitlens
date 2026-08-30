package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/command"
)

const (
	commandPollWait       = 25 * time.Second
	commandExecutionLimit = 60 * time.Second
	commandBackoffInitial = time.Second
	commandBackoffMax     = 30 * time.Second
	commandAgentIDHeader  = "X-PGLENS-Agent-ID"
)

// Executor runs one command kind against a target.
type Executor interface {
	Kind() command.Kind
	Execute(ctx context.Context, t check.Target, args command.Args) (json.RawMessage, error)
}

// RejectedError marks a command that was understood but must not execute.
// The dispatcher maps it to the server's rejected outcome rather than error.
type RejectedError struct{ Reason string }

func (e *RejectedError) Error() string { return e.Reason }

func rejectedf(format string, args ...any) error {
	return &RejectedError{Reason: fmt.Sprintf(format, args...)}
}

type commandTarget struct {
	target              check.Target
	allowExplainAnalyze bool
	allowSignal         bool
}

// Dispatcher long-polls the server once per agent and dispatches commands to
// the executor registered for each command kind.
type Dispatcher struct {
	serverURL string
	token     string
	agentID   uuid.UUID
	enabled   bool
	client    *http.Client
	sleep     func(context.Context, time.Duration) error
	targets   map[uuid.UUID]commandTarget
	executors map[command.Kind]Executor
}

// NewDispatcher creates a command dispatcher. AddTarget and AddExecutor must
// be called before Run.
func NewDispatcher(serverURL, token string, agentID uuid.UUID, enabled bool) *Dispatcher {
	return &Dispatcher{
		serverURL: strings.TrimRight(serverURL, "/"),
		token:     token,
		agentID:   agentID,
		enabled:   enabled,
		client:    &http.Client{Timeout: commandPollWait + 5*time.Second},
		sleep:     sleepCommand,
		targets:   make(map[uuid.UUID]commandTarget),
		executors: make(map[command.Kind]Executor),
	}
}

func sleepCommand(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// AddTarget registers a monitored instance and its command permissions.
func (d *Dispatcher) AddTarget(instanceID uuid.UUID, target check.Target, allowExplainAnalyze, allowSignal bool) {
	d.targets[instanceID] = commandTarget{target: target, allowExplainAnalyze: allowExplainAnalyze, allowSignal: allowSignal}
}

// AddExecutor registers an executor by its command kind.
func (d *Dispatcher) AddExecutor(executor Executor) {
	if executor != nil {
		d.executors[executor.Kind()] = executor
	}
}

// Run polls until ctx is cancelled. A disabled dispatcher returns without
// issuing a request, making commands.enabled=false an actual polling switch.
func (d *Dispatcher) Run(ctx context.Context) {
	if !d.enabled || d.agentID == uuid.Nil {
		return
	}
	backoff := commandBackoffInitial
	for {
		err := d.pollOnce(ctx)
		if err == nil {
			backoff = commandBackoffInitial
			continue
		}
		if ctx.Err() != nil {
			return
		}
		if err := d.sleep(ctx, backoff); err != nil {
			return
		}
		if backoff < commandBackoffMax {
			backoff *= 2
			if backoff > commandBackoffMax {
				backoff = commandBackoffMax
			}
		}
	}
}

type polledCommand struct {
	CommandID  uuid.UUID    `json:"command_id"`
	Kind       command.Kind `json:"kind"`
	Args       command.Args `json:"args"`
	InstanceID uuid.UUID    `json:"instance_id"`
	ClusterID  int64        `json:"cluster_id"`
	ClaimToken uuid.UUID    `json:"claim_token"`
}

type commandResult struct {
	ClaimToken uuid.UUID       `json:"claim_token"`
	Outcome    string          `json:"outcome"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
}

func (d *Dispatcher) pollOnce(ctx context.Context) error {
	endpoint := fmt.Sprintf("%s/api/v1/agents/%s/commands?wait=%s", d.serverURL, d.agentID, url.QueryEscape(commandPollWait.String()))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	d.setHeaders(req)
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("command poll returned %s", resp.Status)
	}
	var polled polledCommand
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&polled); err != nil {
		return fmt.Errorf("decode command poll: %w", err)
	}
	return d.dispatch(ctx, polled)
}

func (d *Dispatcher) dispatch(ctx context.Context, polled polledCommand) error {
	binding, ok := d.targets[polled.InstanceID]
	if !ok {
		return d.postResult(ctx, polled.CommandID, commandResult{ClaimToken: polled.ClaimToken, Outcome: "rejected", Error: fmt.Sprintf("agent does not monitor instance %s", polled.InstanceID)})
	}
	if err := polled.Args.Validate(polled.Kind); err != nil {
		return d.postResult(ctx, polled.CommandID, commandResult{ClaimToken: polled.ClaimToken, Outcome: "rejected", Error: err.Error()})
	}
	// Privilege grants are expected to take effect for the next command while
	// the agent remains running. Managers expose this optional refresh hook so
	// older test targets and lightweight fakes keep working unchanged.
	if refresher, ok := binding.target.(interface {
		RefreshCapabilities(context.Context) error
	}); ok {
		if err := refresher.RefreshCapabilities(ctx); err != nil {
			// Keep the last known conservative capability state on a transient
			// refresh failure; the executor will still enforce its gates.
		}
	}
	gates := command.Gates{
		Tier:                binding.target.PermTier(),
		AllowExplainAnalyze: binding.allowExplainAnalyze,
		AllowSignal:         binding.allowSignal,
		HasPgstattuple:      binding.target.HasExtension("pgstattuple"),
	}
	if allowed, reason := command.Allowed(polled.Kind, polled.Args, gates); !allowed {
		return d.postResult(ctx, polled.CommandID, commandResult{ClaimToken: polled.ClaimToken, Outcome: "rejected", Error: reason})
	}
	executor, ok := d.executors[polled.Kind]
	if !ok {
		return d.postResult(ctx, polled.CommandID, commandResult{ClaimToken: polled.ClaimToken, Outcome: "rejected", Error: fmt.Sprintf("no executor registered for %s", polled.Kind)})
	}
	execCtx, cancel := context.WithTimeout(ctx, commandExecutionLimit)
	result, execErr := executor.Execute(execCtx, binding.target, polled.Args)
	cancel()
	response := commandResult{ClaimToken: polled.ClaimToken, Result: result, Outcome: "ok"}
	if execErr != nil {
		response.Outcome = "error"
		var rejected *RejectedError
		if errors.As(execErr, &rejected) {
			response.Outcome = "rejected"
		}
		response.Result = nil
		response.Error = execErr.Error()
	}
	return d.postResult(ctx, polled.CommandID, response)
}

func (d *Dispatcher) postResult(ctx context.Context, commandID uuid.UUID, result commandResult) error {
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/api/v1/commands/%s/result", d.serverURL, commandID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	d.setHeaders(req)
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("command result returned %s", resp.Status)
	}
	return nil
}

func (d *Dispatcher) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+d.token)
	req.Header.Set(commandAgentIDHeader, d.agentID.String())
}
