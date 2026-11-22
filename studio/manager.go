package studio

import (
	"bufio"
	"cmp"
	_ "embed"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

//go:embed worker.py
var embeddedWorkerPy string

type workerResponse struct {
	ID      int64  `json:"id"`
	Event   string `json:"event"`
	Message string `json:"message"`
	Result  []byte `json:"result"`
	Error   string `json:"error"`
}

// Manager owns the Python sidecar worker process and studio session lifecycle.
type Manager struct {
	mu           sync.Mutex
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	pending      map[int64]chan workerReply
	pendingMu    sync.Mutex
	nextID       atomic.Int64
	activeTarget string
	connected    bool
	sessionLabel string
	pythonPath   string
	sourceLabel  string
	workerFile   string
	onEvent      func(event string, data any)
}

type workerReply struct {
	result []byte
	err    error
}

// NewManager initializes a Studio manager that emits real-time events via onEvent.
func NewManager(onEvent func(event string, data any)) *Manager {
	return &Manager{
		pending: make(map[int64]chan workerReply),
		onEvent: onEvent,
	}
}

func (m *Manager) emit(event string, data any) {
	if m.onEvent != nil {
		m.onEvent(event, data)
	}
}

// Status returns the current interactive session status.
func (m *Manager) Status() SessionStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return SessionStatus{
		Connected: m.connected,
		Target:    m.activeTarget,
		Label:     m.sessionLabel,
		Python:    m.pythonPath,
		Source:    m.sourceLabel,
	}
}

// StartSession launches (or reuses) the Python worker for cfg.Target and connects to the target.
func (m *Manager) StartSession(cfg SessionConfig) (Snapshot, error) {
	target := cmp.Or(strings.TrimSpace(cfg.Target), "android")
	info := GetSystemInfo()

	var tool ToolStatus
	if target == "web" {
		tool = info.PyestroWeb
	} else {
		target = "android"
		tool = info.Pyestro
		if !info.ADB.Found {
			return Snapshot{}, errors.New("adb was not found. Install Android SDK Platform Tools and ensure adb is on PATH.")
		}
	}

	if !tool.Found || tool.PythonPath == "" {
		return Snapshot{}, fmt.Errorf("%s was not found. Install it via `uv tool install` in Environment & uv.", tool.Name)
	}

	if err := m.ensureWorker(target, tool.PythonPath, tool.Source); err != nil {
		return Snapshot{}, err
	}

	params := map[string]any{
		"serial":   cfg.Serial,
		"appId":    cfg.AppID,
		"adbPath":  info.ADB.Path,
		"url":      cmp.Or(cfg.URL, "http://localhost:8081"),
		"browser":  cmp.Or(cfg.Browser, "chromium"),
		"viewport": cmp.Or(cfg.Viewport, "mobile"),
		"headless": cfg.Headless,
	}

	raw, err := m.callWorker("start_session", params)
	if err != nil {
		m.mu.Lock()
		m.connected = false
		m.mu.Unlock()
		m.emit("studio:status", m.Status())
		return Snapshot{}, err
	}

	var resp struct {
		Connected bool     `json:"connected"`
		Snapshot  Snapshot `json:"snapshot"`
		Logs      []string `json:"logs"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Snapshot{}, fmt.Errorf("decode session snapshot: %w", err)
	}

	label := cmp.Or(cfg.Serial, "Default Android Device")
	if target == "web" {
		label = fmt.Sprintf("%s (%s)", cmp.Or(cfg.URL, "http://localhost:8081"), cmp.Or(cfg.Browser, "chromium"))
	}

	m.mu.Lock()
	m.connected = resp.Connected
	m.sessionLabel = label
	m.mu.Unlock()

	m.emit("studio:status", m.Status())
	m.emit("studio:snapshot", resp.Snapshot)
	return resp.Snapshot, nil
}

// StopSession closes the active interactive driver and terminates the worker.
func (m *Manager) StopSession() SessionStatus {
	_, _ = m.callWorker("stop_session", nil)
	m.stopWorker()
	status := m.Status()
	m.emit("studio:status", status)
	return status
}

// RefreshSnapshot captures a fresh screenshot and element hierarchy from the active session.
func (m *Manager) RefreshSnapshot() (Snapshot, error) {
	raw, err := m.callWorker("snapshot", nil)
	if err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return Snapshot{}, fmt.Errorf("decode snapshot: %w", err)
	}
	m.emit("studio:snapshot", snap)
	return snap, nil
}

// RunStep executes a single Maestro YAML snippet on the active session.
func (m *Manager) RunStep(req StepRequest) (StepResponse, error) {
	raw, err := m.callWorker("run_step", map[string]any{
		"yaml":     req.YAML,
		"flowPath": req.FlowPath,
	})
	if err != nil {
		return StepResponse{}, err
	}
	var resp StepResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return StepResponse{}, fmt.Errorf("decode step response: %w", err)
	}
	if resp.Snapshot != nil {
		m.emit("studio:snapshot", resp.Snapshot)
	}
	return resp, nil
}

// ValidateYAML validates a Maestro flow YAML string using the target engine.
func (m *Manager) ValidateYAML(req ValidateRequest) (ValidateResponse, error) {
	target := cmp.Or(strings.TrimSpace(req.Target), "android")
	info := GetSystemInfo()
	tool := info.Pyestro
	if target == "web" {
		tool = info.PyestroWeb
	}
	if !tool.Found || tool.PythonPath == "" {
		return ValidateResponse{}, fmt.Errorf("%s was not found. Install it via `uv tool install` in Environment & uv.", tool.Name)
	}

	if err := m.ensureWorker(target, tool.PythonPath, tool.Source); err != nil {
		return ValidateResponse{}, err
	}

	raw, err := m.callWorker("validate_yaml", map[string]any{
		"yaml":     req.YAML,
		"flowPath": req.FlowPath,
	})
	if err != nil {
		return ValidateResponse{Valid: false, Error: err.Error(), Engine: tool.Name}, nil
	}
	var resp ValidateResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ValidateResponse{}, fmt.Errorf("decode validation response: %w", err)
	}
	return resp, nil
}

// RunFlow runs a complete Maestro YAML flow using the configured engine.
func (m *Manager) RunFlow(req RunFlowRequest) (RunFlowResponse, error) {
	target := cmp.Or(strings.TrimSpace(req.Target), "android")
	info := GetSystemInfo()
	tool := info.Pyestro
	if target == "web" {
		tool = info.PyestroWeb
	}
	if !tool.Found || tool.PythonPath == "" {
		return RunFlowResponse{}, fmt.Errorf("%s was not found. Install it via `uv tool install` in Environment & uv.", tool.Name)
	}
	if target == "android" && !info.ADB.Found {
		return RunFlowResponse{}, errors.New("adb was not found. Install Android SDK Platform Tools and ensure adb is on PATH.")
	}

	if err := m.ensureWorker(target, tool.PythonPath, tool.Source); err != nil {
		return RunFlowResponse{}, err
	}

	raw, err := m.callWorker("run_flow_yaml", map[string]any{
		"yaml":     req.YAML,
		"flowPath": req.FlowPath,
		"serial":   req.Serial,
		"adbPath":  info.ADB.Path,
		"url":      req.URL,
		"browser":  cmp.Or(req.Browser, "chromium"),
		"viewport": cmp.Or(req.Viewport, "mobile"),
		"headless": req.Headless,
		"record":   req.Record,
		"report":   req.Report,
	})
	if err != nil {
		return RunFlowResponse{}, err
	}

	var resp struct {
		RunFlowResponse
		Snapshot *Snapshot `json:"snapshot,omitempty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return RunFlowResponse{}, fmt.Errorf("decode run flow response: %w", err)
	}
	if resp.Snapshot != nil {
		m.emit("studio:snapshot", resp.Snapshot)
	}
	return resp.RunFlowResponse, nil
}

// UVInstallCommand returns the OS-specific command to install uv.
func UVInstallCommand(goos string, hasCmd func(string) bool) (string, []string, string, error) {
	switch goos {
	case "darwin", "linux":
		if hasCmd("curl") {
			script := "curl -LsSf https://astral.sh/uv/install.sh | sh"
			return "sh", []string{"-c", script}, script, nil
		}
		if hasCmd("wget") {
			script := "wget -qO- https://astral.sh/uv/install.sh | sh"
			return "sh", []string{"-c", script}, script, nil
		}
		return "", nil, "", errors.New("neither curl nor wget was found to download the uv installer")
	case "windows":
		psBin := ""
		switch {
		case hasCmd("powershell"):
			psBin = "powershell"
		case hasCmd("pwsh"):
			psBin = "pwsh"
		default:
			return "", nil, "", errors.New("PowerShell was not found to install uv")
		}
		psScript := "irm https://astral.sh/uv/install.ps1 | iex"
		args := []string{"-NoProfile", "-ExecutionPolicy", "ByPass", "-Command", psScript}
		display := fmt.Sprintf(`%s -ExecutionPolicy ByPass -c "%s"`, psBin, psScript)
		return psBin, args, display, nil
	default:
		return "", nil, "", fmt.Errorf("automatic uv installation is not supported on %s", goos)
	}
}

func hasCommandOnPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func (m *Manager) installUV() InstallToolResponse {
	bin, args, cmdStr, err := UVInstallCommand(runtime.GOOS, hasCommandOnPath)
	if err != nil {
		return InstallToolResponse{
			OK:    false,
			Error: err.Error(),
		}
	}

	out, runErr := exec.Command(bin, args...).CombinedOutput()
	output := strings.TrimSpace(string(out))
	if runErr != nil {
		return InstallToolResponse{
			OK:      false,
			Command: cmdStr,
			Output:  output,
			Error:   runErr.Error(),
		}
	}

	return InstallToolResponse{
		OK:      true,
		Command: cmdStr,
		Output:  output,
	}
}

// InstallToolViaUV installs uv (OS-detected) or runs `uv tool install --upgrade`
// for pyestro or pyestro-web (plus Playwright Chromium for pyestro-web).
func (m *Manager) InstallToolViaUV(target string) (InstallToolResponse, error) {
	if target == "uv" {
		return m.installUV(), nil
	}
	if target != "pyestro" && target != "pyestro-web" {
		return InstallToolResponse{}, fmt.Errorf("unsupported tool %q", target)
	}
	uvPath, _ := LocateBinary("uv")
	if uvPath == "" {
		return InstallToolResponse{}, errors.New("uv binary was not found on PATH")
	}

	repoURL := fmt.Sprintf("git+https://github.com/carlosxfelipe/%s.git", target)
	args := []string{"tool", "install", "--upgrade", repoURL}
	cmd := exec.Command(uvPath, args...)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	cmdStr := fmt.Sprintf("%s %s", filepath.Base(uvPath), strings.Join(args, " "))
	if err != nil {
		return InstallToolResponse{
			OK:      false,
			Command: cmdStr,
			Output:  output,
			Error:   err.Error(),
		}, nil
	}

	if target == "pyestro-web" {
		pwArgs := []string{"tool", "run", "--from", "pyestro-web", "playwright", "install", "chromium"}
		pwCmd := exec.Command(uvPath, pwArgs...)
		pwOut, pwErr := pwCmd.CombinedOutput()
		pwOutput := strings.TrimSpace(string(pwOut))
		pwCmdStr := fmt.Sprintf("%s %s", filepath.Base(uvPath), strings.Join(pwArgs, " "))
		cmdStr = cmdStr + " && " + pwCmdStr
		if pwOutput != "" {
			if output != "" {
				output += "\n\n"
			}
			output += pwOutput
		}
		if pwErr != nil {
			return InstallToolResponse{
				OK:      false,
				Command: cmdStr,
				Output:  output,
				Error:   pwErr.Error(),
			}, nil
		}
	}

	return InstallToolResponse{
		OK:      true,
		Command: cmdStr,
		Output:  output,
	}, nil
}

// Shutdown cleanly terminates any running Python sidecar process.
func (m *Manager) Shutdown() {
	_, _ = m.callWorker("shutdown", nil)
	m.stopWorker()
}

func (m *Manager) ensureWorker(target string, pythonPath string, sourceLabel string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cmd != nil && m.cmd.Process != nil && m.activeTarget == target && m.pythonPath == pythonPath {
		return nil
	}

	m.stopWorkerLocked()

	workerPath, err := m.ensureWorkerScriptLocked()
	if err != nil {
		return err
	}

	cmd := exec.Command(pythonPath, "-u", workerPath)
	cmd.Env = append(os.Environ(), "PYESTRO_STUDIO_TARGET="+target)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("worker stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("worker stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("worker stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start python worker (%s): %w", pythonPath, err)
	}

	m.cmd = cmd
	m.stdin = stdin
	m.activeTarget = target
	m.pythonPath = pythonPath
	m.sourceLabel = sourceLabel

	go m.readStdout(stdout)
	go m.readStderr(stderr)
	go func(c *exec.Cmd) {
		_ = c.Wait()
		m.mu.Lock()
		if m.cmd == c {
			m.cmd = nil
			m.stdin = nil
			m.connected = false
		}
		m.mu.Unlock()
		m.failPending(errors.New("python studio worker exited"))
		m.emit("studio:status", m.Status())
	}(cmd)

	return nil
}

func (m *Manager) ensureWorkerScriptLocked() (string, error) {
	if m.workerFile != "" {
		if _, err := os.Stat(m.workerFile); err == nil {
			return m.workerFile, nil
		}
	}
	dir := filepath.Join(os.TempDir(), "pyestro-studio")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create worker temp dir: %w", err)
	}
	scriptPath := filepath.Join(dir, "worker.py")
	if err := os.WriteFile(scriptPath, []byte(embeddedWorkerPy), 0644); err != nil {
		return "", fmt.Errorf("write worker script: %w", err)
	}
	m.workerFile = scriptPath
	return scriptPath, nil
}

func (m *Manager) callWorker(method string, params map[string]any) ([]byte, error) {
	m.mu.Lock()
	stdin := m.stdin
	m.mu.Unlock()
	if stdin == nil {
		return nil, errors.New("python studio worker is not running")
	}

	id := m.nextID.Add(1)
	ch := make(chan workerReply, 1)

	m.pendingMu.Lock()
	m.pending[id] = ch
	m.pendingMu.Unlock()

	payload := map[string]any{
		"id":     id,
		"method": method,
		"params": params,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		m.removePending(id)
		return nil, err
	}

	m.mu.Lock()
	if m.stdin == nil {
		m.mu.Unlock()
		m.removePending(id)
		return nil, errors.New("python studio worker closed")
	}
	_, writeErr := m.stdin.Write(append(raw, '\n'))
	m.mu.Unlock()
	if writeErr != nil {
		m.removePending(id)
		return nil, writeErr
	}

	reply := <-ch
	return reply.result, reply.err
}

func (m *Manager) removePending(id int64) {
	m.pendingMu.Lock()
	delete(m.pending, id)
	m.pendingMu.Unlock()
}

func (m *Manager) failPending(err error) {
	m.pendingMu.Lock()
	for id, ch := range m.pending {
		ch <- workerReply{err: err}
		delete(m.pending, id)
	}
	m.pendingMu.Unlock()
}

func (m *Manager) readStdout(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var env struct {
			ID       int64          `json:"id"`
			Event    string         `json:"event"`
			Message  string         `json:"message"`
			Snapshot *Snapshot      `json:"snapshot"`
			Result   jsontext.Value `json:"result"`
			Error    string         `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			m.emit("studio:log", line)
			continue
		}
		if env.Event == "log" {
			m.emit("studio:log", env.Message)
			continue
		}
		if env.Event == "snapshot" && env.Snapshot != nil {
			m.emit("studio:snapshot", env.Snapshot)
			continue
		}
		if env.ID != 0 {
			m.pendingMu.Lock()
			ch, ok := m.pending[env.ID]
			if ok {
				delete(m.pending, env.ID)
			}
			m.pendingMu.Unlock()
			if ok {
				if env.Error != "" {
					ch <- workerReply{err: errors.New(env.Error)}
				} else {
					ch <- workerReply{result: env.Result}
				}
			}
		}
	}
}

func (m *Manager) readStderr(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			m.emit("studio:log", "[stderr] "+line)
		}
	}
}

func (m *Manager) stopWorker() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopWorkerLocked()
}

func (m *Manager) stopWorkerLocked() {
	if m.stdin != nil {
		_ = m.stdin.Close()
		m.stdin = nil
	}
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
	}
	m.cmd = nil
	m.connected = false
	m.activeTarget = ""
	m.sessionLabel = ""
	m.pythonPath = ""
	m.sourceLabel = ""
}
