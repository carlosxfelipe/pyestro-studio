package studio

// ToolStatus reports discovery status for an external binary or Python package.
type ToolStatus struct {
	Name       string `json:"name"`
	Found      bool   `json:"found"`
	Path       string `json:"path"`
	PythonPath string `json:"pythonPath,omitempty"`
	Version    string `json:"version"`
	Source     string `json:"source"`
	Hint       string `json:"hint"`
}

// AndroidDevice describes an ADB-connected emulator or physical device.
type AndroidDevice struct {
	Serial string `json:"serial"`
	State  string `json:"state"`
}

// SystemInfo aggregates host capabilities, tool detection, and packaging guidance.
type SystemInfo struct {
	OS            string          `json:"os"`
	Arch          string          `json:"arch"`
	PackagingNote string          `json:"packagingNote"`
	UV            ToolStatus      `json:"uv"`
	ADB           ToolStatus      `json:"adb"`
	Pyestro       ToolStatus      `json:"pyestro"`
	PyestroWeb    ToolStatus      `json:"pyestroWeb"`
	AndroidReady  bool            `json:"androidReady"`
	WebReady      bool            `json:"webReady"`
	Devices       []AndroidDevice `json:"devices"`
}

// SessionConfig configures an interactive Android or Web inspection session.
type SessionConfig struct {
	Target   string `json:"target"`   // "android" or "web"
	Serial   string `json:"serial"`   // Android ADB serial
	AppID    string `json:"appId"`    // Optional Android app ID
	URL      string `json:"url"`      // Web base URL
	Browser  string `json:"browser"`  // "chromium", "firefox", "webkit"
	Viewport string `json:"viewport"` // "mobile", "desktop", "tablet", or "WxH"
	Headless bool   `json:"headless"` // Browser headless mode
}

// SessionStatus describes the currently active sidecar session.
type SessionStatus struct {
	Connected bool   `json:"connected"`
	Target    string `json:"target"`
	Label     string `json:"label"`
	Python    string `json:"python,omitempty"`
	Source    string `json:"source,omitempty"`
	Error     string `json:"error,omitempty"`
}

// UIElement represents a flattened Android accessibility node or Web DOM element.
type UIElement struct {
	Index       int      `json:"index"`
	Parent      *int     `json:"parent,omitempty"`
	Text        string   `json:"text"`
	Hint        string   `json:"hint"`
	ResourceID  string   `json:"resourceId"`
	ContentDesc string   `json:"contentDesc"`
	ClassName   string   `json:"className"`
	DOMID       string   `json:"domId,omitempty"`
	Bounds      [4]int   `json:"bounds"`
	Clickable   bool     `json:"clickable"`
	Enabled     bool     `json:"enabled"`
	Checked     bool     `json:"checked"`
	Focused     bool     `json:"focused"`
	Selected    bool     `json:"selected"`
	Visible     bool     `json:"visible"`
	Selectors   []string `json:"selectors"`
}

// Snapshot holds a captured screenshot and its corresponding interactive UI elements.
type Snapshot struct {
	Target      string      `json:"target"`
	Width       int         `json:"width"`
	Height      int         `json:"height"`
	ImageData   string      `json:"imageData"`
	CurrentURL  string      `json:"currentUrl,omitempty"`
	Elements    []UIElement `json:"elements"`
	CapturedAt  string      `json:"capturedAt"`
	ElementHint string      `json:"elementHint,omitempty"`
}

// StepRequest executes a single Maestro YAML command snippet against the active session.
type StepRequest struct {
	YAML     string            `json:"yaml"`
	FlowPath string            `json:"flowPath,omitempty"`
	FlowYAML string            `json:"flowYaml,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	EnvFile  string            `json:"envFile,omitempty"`
}

// StepResponse returns the outcome of executing an interactive step.
type StepResponse struct {
	OK         bool      `json:"ok"`
	Command    string    `json:"command"`
	DurationMS int64     `json:"durationMs"`
	Error      string    `json:"error,omitempty"`
	Logs       []string  `json:"logs"`
	Snapshot   *Snapshot `json:"snapshot,omitempty"`
}

// ValidateRequest checks Maestro YAML syntax using the target Python engine.
type ValidateRequest struct {
	Target   string            `json:"target"`
	YAML     string            `json:"yaml"`
	FlowPath string            `json:"flowPath,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	EnvFile  string            `json:"envFile,omitempty"`
}

// ValidateResponse summarizes YAML validation results.
type ValidateResponse struct {
	Valid  bool     `json:"valid"`
	AppID  string   `json:"appId,omitempty"`
	Name   string   `json:"name,omitempty"`
	URL    string   `json:"url,omitempty"`
	Tags   []string `json:"tags"`
	Steps  int      `json:"steps"`
	Error  string   `json:"error,omitempty"`
	Engine string   `json:"engine"`
}

// RunFlowRequest runs a complete Maestro YAML flow using the configured runner.
type RunFlowRequest struct {
	Target   string            `json:"target"`
	YAML     string            `json:"yaml"`
	FlowPath string            `json:"flowPath,omitempty"`
	Serial   string            `json:"serial,omitempty"`
	URL      string            `json:"url,omitempty"`
	Browser  string            `json:"browser,omitempty"`
	Viewport string            `json:"viewport,omitempty"`
	Headless bool              `json:"headless"`
	Record   bool              `json:"record,omitzero"`
	Report   bool              `json:"report,omitzero"`
	Env      map[string]string `json:"env,omitempty"`
	EnvFile  string            `json:"envFile,omitempty"`
}

// EnvConfig stores per-flow or default environment overrides and .env file path.
type EnvConfig struct {
	FlowPath string            `json:"flowPath,omitempty"`
	EnvFile  string            `json:"envFile,omitempty"`
	RawEnv   string            `json:"rawEnv,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
}

// RunFlowResponse reports the completed flow execution summary.
type RunFlowResponse struct {
	OK         bool     `json:"ok"`
	Passed     int      `json:"passed"`
	Failed     int      `json:"failed"`
	Skipped    int      `json:"skipped"`
	Total      int      `json:"total"`
	DurationMS int64    `json:"durationMs"`
	Error      string   `json:"error,omitempty"`
	Logs       []string `json:"logs"`
	ReportPath string   `json:"reportPath,omitempty"`
	VideoPath  string   `json:"videoPath,omitempty"`
}

// BrowseResponse returns a selected file or directory path from a native dialog.
type BrowseResponse struct {
	Path string `json:"path"`
}

// FlowFileResponse returns loaded YAML content and metadata.
type FlowFileResponse struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Hash    string `json:"hash,omitempty"`
}

// SaveFlowRequest saves YAML content to disk.
type SaveFlowRequest struct {
	Path         string `json:"path"`
	Content      string `json:"content"`
	ExpectedHash string `json:"expectedHash,omitempty"`
	Force        bool   `json:"force,omitempty"`
}

// InstallToolRequest installs or upgrades uv, pyestro, or pyestro-web.
type InstallToolRequest struct {
	Target string `json:"target"` // "uv", "pyestro", or "pyestro-web"
}

// InstallToolResponse reports the result of running uv tool install/upgrade.
type InstallToolResponse struct {
	OK      bool   `json:"ok"`
	Command string `json:"command"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}
