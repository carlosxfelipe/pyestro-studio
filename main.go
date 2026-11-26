package main

import (
	"cmp"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"pyestro-studio/store"
	"pyestro-studio/studio"

	"github.com/crgimenes/glaze"
	"github.com/crgimenes/glaze/menu"
)

//go:embed all:ui assets/icon.png
var embeddedAssets embed.FS

func init() {
	runtime.LockOSThread()
}

// AppAPI exposes backend methods to JavaScript via glaze.BindMethods under the "api_" prefix.
type AppAPI struct {
	webview glaze.WebView
	mgr     *studio.Manager
	events  *glaze.Events
	store   *store.Store
}

// SystemInfo detects host tools (uv, adb, pyestro, pyestro-web) and ADB devices.
func (a *AppAPI) SystemInfo() studio.SystemInfo {
	return studio.GetSystemInfo()
}

// OpenFlowFile opens a native file picker for a Maestro YAML flow and reads its content.
func (a *AppAPI) OpenFlowFile() (studio.FlowFileResponse, error) {
	if a.webview == nil {
		return studio.FlowFileResponse{}, errors.New("webview unavailable")
	}
	selected, err := a.webview.OpenFile(glaze.FileDialogOptions{
		Title: "Open Maestro YAML Flow",
		Filters: []glaze.FileFilter{
			{Name: "Maestro YAML Flows", Extensions: []string{"yaml", "yml"}},
			{Name: "All Files", Extensions: []string{"*"}},
		},
	})
	if err != nil || selected == "" {
		return studio.FlowFileResponse{}, err
	}
	raw, err := os.ReadFile(selected)
	if err != nil {
		return studio.FlowFileResponse{}, err
	}
	return studio.FlowFileResponse{
		Path:    selected,
		Content: string(raw),
		Hash:    hashBytes(raw),
	}, nil
}

// OpenProjectFolder opens a native folder picker and scans the chosen project for Maestro YAML flows (including .maestro/) and .env files.
func (a *AppAPI) OpenProjectFolder() (studio.ProjectScanResponse, error) {
	if a.webview == nil {
		return studio.ProjectScanResponse{}, errors.New("webview unavailable")
	}
	selected, err := a.webview.OpenDirectory(glaze.FileDialogOptions{
		Title: "Open Project Folder",
	})
	if err != nil || selected == "" {
		return studio.ProjectScanResponse{}, err
	}
	return scanProjectDir(selected)
}

// ScanProjectFolder rescans a previously selected project directory for YAML flows and .env files.
func (a *AppAPI) ScanProjectFolder(root string) (studio.ProjectScanResponse, error) {
	cleanRoot := strings.TrimSpace(root)
	if cleanRoot == "" {
		return studio.ProjectScanResponse{}, errors.New("project folder path is empty")
	}
	return scanProjectDir(cleanRoot)
}

// ReadFlowFile reads a YAML flow file from disk and returns its content and hash.
func (a *AppAPI) ReadFlowFile(filePath string) (studio.FlowFileResponse, error) {
	cleanPath := strings.TrimSpace(filePath)
	if cleanPath == "" {
		return studio.FlowFileResponse{}, errors.New("flow file path is empty")
	}
	raw, err := os.ReadFile(cleanPath)
	if err != nil {
		return studio.FlowFileResponse{}, err
	}
	return studio.FlowFileResponse{
		Path:    cleanPath,
		Content: string(raw),
		Hash:    hashBytes(raw),
	}, nil
}

var skippedProjectDirs = map[string]bool{
	"node_modules": true,
	"__pycache__":  true,
	"DerivedData":  true,
	"Pods":         true,
	"build":        true,
	"dist":         true,
	"target":       true,
	"artifacts":    true,
	"venv":         true,
}

func scanProjectDir(root string) (studio.ProjectScanResponse, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return studio.ProjectScanResponse{}, err
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return studio.ProjectScanResponse{}, err
	}
	if !info.IsDir() {
		return studio.ProjectScanResponse{}, fmt.Errorf("not a directory: %s", absRoot)
	}

	const maxEntries = 200
	const maxDepth = 5

	res := studio.ProjectScanResponse{
		Root:     absRoot,
		Name:     filepath.Base(absRoot),
		Flows:    []studio.ProjectFlowEntry{},
		EnvFiles: []studio.ProjectFlowEntry{},
	}

	_ = filepath.WalkDir(absRoot, func(currPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if currPath == absRoot {
			return nil
		}
		rel, err := filepath.Rel(absRoot, currPath)
		if err != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		name := d.Name()
		depth := strings.Count(relSlash, "/") + 1

		if d.IsDir() {
			if depth > maxDepth || skippedProjectDirs[name] {
				return filepath.SkipDir
			}
			// Skip hidden directories except .maestro so dot-folder flows are always discovered.
			if strings.HasPrefix(name, ".") && name != ".maestro" {
				return filepath.SkipDir
			}
			return nil
		}

		if len(res.Flows)+len(res.EnvFiles) >= maxEntries {
			return fs.SkipAll
		}

		inMaestro := relSlash == ".maestro" || strings.HasPrefix(relSlash, ".maestro/")
		lower := strings.ToLower(name)
		if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
			res.Flows = append(res.Flows, studio.ProjectFlowEntry{
				Name:    name,
				RelPath: relSlash,
				Path:    currPath,
				InDot:   inMaestro,
			})
			return nil
		}

		if name == ".env" || strings.HasPrefix(name, ".env.") {
			res.EnvFiles = append(res.EnvFiles, studio.ProjectFlowEntry{
				Name:    name,
				RelPath: relSlash,
				Path:    currPath,
				InDot:   inMaestro,
			})
		}
		return nil
	})

	sortEntries := func(entries []studio.ProjectFlowEntry) {
		slices.SortFunc(entries, func(a, b studio.ProjectFlowEntry) int {
			if a.InDot != b.InDot {
				if a.InDot {
					return -1
				}
				return 1
			}
			return cmp.Compare(a.RelPath, b.RelPath)
		})
	}
	sortEntries(res.Flows)
	sortEntries(res.EnvFiles)

	return res, nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// SaveFlowFile writes YAML flow content to the specified path (or prompts Save As when empty).
// When overwriting an existing file, it requires Force or a matching ExpectedHash to prevent accidental clobbering.
func (a *AppAPI) SaveFlowFile(req studio.SaveFlowRequest) (studio.FlowFileResponse, error) {
	targetPath := strings.TrimSpace(req.Path)
	if targetPath == "" {
		return a.SaveFlowFileAs(req)
	}
	if !req.Force {
		if existing, err := os.ReadFile(targetPath); err == nil {
			currentHash := hashBytes(existing)
			if req.ExpectedHash == "" {
				return studio.FlowFileResponse{}, fmt.Errorf("refusing to overwrite existing file without confirmation: %s", targetPath)
			}
			if currentHash != req.ExpectedHash {
				return studio.FlowFileResponse{}, fmt.Errorf("file changed on disk since it was opened: %s", targetPath)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return studio.FlowFileResponse{}, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return studio.FlowFileResponse{}, err
	}
	data := []byte(req.Content)
	if err := os.WriteFile(targetPath, data, 0644); err != nil {
		return studio.FlowFileResponse{}, err
	}
	return studio.FlowFileResponse{
		Path:    targetPath,
		Content: req.Content,
		Hash:    hashBytes(data),
	}, nil
}

// SaveFlowFileAs opens a native Save dialog and writes the YAML flow file.
func (a *AppAPI) SaveFlowFileAs(req studio.SaveFlowRequest) (studio.FlowFileResponse, error) {
	if a.webview == nil {
		return studio.FlowFileResponse{}, errors.New("webview unavailable")
	}
	filename := "flow.yaml"
	dir := ""
	if p := strings.TrimSpace(req.Path); p != "" {
		filename = filepath.Base(p)
		dir = filepath.Dir(p)
	}
	selected, err := a.webview.SaveFile(glaze.FileDialogOptions{
		Title:     "Save Maestro YAML Flow",
		Directory: dir,
		Filename:  filename,
		Filters: []glaze.FileFilter{
			{Name: "Maestro YAML Flows", Extensions: []string{"yaml", "yml"}},
		},
	})
	if err != nil || selected == "" {
		return studio.FlowFileResponse{}, err
	}
	data := []byte(req.Content)
	if err := os.WriteFile(selected, data, 0644); err != nil {
		return studio.FlowFileResponse{}, err
	}
	return studio.FlowFileResponse{
		Path:    selected,
		Content: req.Content,
		Hash:    hashBytes(data),
	}, nil
}

// SessionStatus returns the active Studio session status.
func (a *AppAPI) SessionStatus() studio.SessionStatus {
	return a.mgr.Status()
}

// StartSession starts an interactive Android or Web studio session and captures an initial snapshot.
func (a *AppAPI) StartSession(cfg studio.SessionConfig) (studio.Snapshot, error) {
	return a.mgr.StartSession(cfg)
}

// StopSession closes the active interactive studio session.
func (a *AppAPI) StopSession() studio.SessionStatus {
	return a.mgr.StopSession()
}

// RefreshSnapshot captures a fresh screenshot and element hierarchy from the active session.
func (a *AppAPI) RefreshSnapshot() (studio.Snapshot, error) {
	return a.mgr.RefreshSnapshot()
}

// RunStep runs a single Maestro YAML snippet against the active session.
func (a *AppAPI) RunStep(req studio.StepRequest) (studio.StepResponse, error) {
	return a.mgr.RunStep(req)
}

// ValidateYAML validates a Maestro YAML flow using the configured engine.
func (a *AppAPI) ValidateYAML(req studio.ValidateRequest) (studio.ValidateResponse, error) {
	return a.mgr.ValidateYAML(req)
}

// RunFlow executes a full Maestro YAML flow.
func (a *AppAPI) RunFlow(req studio.RunFlowRequest) (studio.RunFlowResponse, error) {
	return a.mgr.RunFlow(req)
}

// InstallTool installs uv (for the current OS) or runs `uv tool install --upgrade` for pyestro or pyestro-web.
func (a *AppAPI) InstallTool(req studio.InstallToolRequest) (studio.InstallToolResponse, error) {
	return a.mgr.InstallToolViaUV(req.Target)
}

// OpenEnvFile opens a native file picker for selecting a .env file.
func (a *AppAPI) OpenEnvFile() (studio.BrowseResponse, error) {
	if a.webview == nil {
		return studio.BrowseResponse{}, errors.New("webview unavailable")
	}
	selected, err := a.webview.OpenFile(glaze.FileDialogOptions{
		Title: "Select .env File",
		Filters: []glaze.FileFilter{
			{Name: "All Files", Extensions: []string{"*"}},
		},
	})
	if err != nil || selected == "" {
		return studio.BrowseResponse{}, err
	}
	return studio.BrowseResponse{Path: selected}, nil
}

func envStoreKey(flowPath string) string {
	trimmed := strings.TrimSpace(flowPath)
	if trimmed == "" {
		return "env:__default__"
	}
	return "env:" + filepath.Clean(trimmed)
}

// GetEnvConfig loads persisted .env file and variable overrides for a flow (or default).
func (a *AppAPI) GetEnvConfig(flowPath string) (studio.EnvConfig, error) {
	if a.store == nil {
		return studio.EnvConfig{FlowPath: flowPath}, nil
	}
	var cfg studio.EnvConfig
	ok, err := a.store.Get(envStoreKey(flowPath), &cfg)
	if err != nil {
		return studio.EnvConfig{}, err
	}
	if !ok {
		return studio.EnvConfig{FlowPath: flowPath}, nil
	}
	cfg.FlowPath = flowPath
	return cfg, nil
}

// SaveEnvConfig persists .env file and variable overrides for a flow (or default).
func (a *AppAPI) SaveEnvConfig(cfg studio.EnvConfig) error {
	if a.store == nil {
		return nil
	}
	return a.store.Set(envStoreKey(cfg.FlowPath), cfg)
}

// OpenExternal opens a URL or file path using the OS default handler.
func (a *AppAPI) OpenExternal(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil
	}
	return openExternal(target)
}

// CopyText copies text to the system clipboard.
func (a *AppAPI) CopyText(text string) error {
	return copyToClipboard(text)
}

func serveAsset(req *glaze.SchemeRequest) *glaze.SchemeResponse {
	name := assetName(req.URL)
	if name == "icon.png" {
		data, err := embeddedAssets.ReadFile("assets/icon.png")
		if err != nil {
			return nil
		}
		return &glaze.SchemeResponse{Body: data, MIMEType: "image/png"}
	}

	data, err := embeddedAssets.ReadFile("ui/" + name)
	if err != nil {
		if path.Ext(name) != "" {
			return nil
		}
		data, err = embeddedAssets.ReadFile("ui/index.html")
		if err != nil {
			return nil
		}
		name = "index.html"
	}

	return &glaze.SchemeResponse{
		Body:     data,
		MIMEType: mimeTypeFor(name),
	}
}

func assetName(reqURL string) string {
	p := reqURL
	if u, err := url.Parse(reqURL); err == nil && u.Path != "" {
		p = u.Path
	}
	p = strings.TrimPrefix(path.Clean("/"+p), "/")
	if p == "" || p == "." {
		return "index.html"
	}
	return p
}

func mimeTypeFor(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".png":
		return "image/png"
	case ".svg":
		return "image/svg+xml"
	case ".json":
		return "application/json; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func openExternal(target string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", target).Start()
	case "linux":
		return exec.Command("xdg-open", target).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func copyToClipboard(text string) error {
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	case "linux":
		for _, tool := range []struct {
			bin  string
			args []string
		}{
			{"wl-copy", nil},
			{"xclip", []string{"-selection", "clipboard"}},
			{"xsel", []string{"--clipboard", "--input"}},
		} {
			if p, err := exec.LookPath(tool.bin); err == nil {
				cmd := exec.Command(p, tool.args...)
				cmd.Stdin = strings.NewReader(text)
				return cmd.Run()
			}
		}
	}
	return errors.New("no clipboard utility found")
}

func main() {
	headless := os.Getenv("GLAZE_HEADLESS") == "1"
	debug := os.Getenv("GLAZE_DEBUG") == "1"

	if iconBytes, err := embeddedAssets.ReadFile("assets/icon.png"); err == nil {
		if err := glaze.SetAppIcon(iconBytes); err != nil && !errors.Is(err, glaze.ErrIconUnsupported) {
			log.Printf("Warning setting app icon: %v", err)
		}
	}

	var events *glaze.Events
	mgr := studio.NewManager(func(event string, data any) {
		if events != nil {
			_ = events.Emit(event, data)
		}
	})

	done := make(chan struct{})
	var w glaze.WebView

	var err error
	w, err = glaze.NewWithOptions(glaze.Options{
		Debug:           debug,
		Hidden:          headless,
		HideUntilLoaded: !headless,
		SchemeHandlers: map[string]glaze.SchemeHandler{
			"app": serveAsset,
		},
		OnNewWindow: func(target string) {
			_ = openExternal(target)
		},
		OnClose: func() {
			close(done)
		},
	})
	if err != nil {
		log.Fatalf("glaze init: %v", err)
	}
	defer w.Destroy()

	events, err = glaze.NewEvents(w)
	if err != nil {
		log.Fatalf("glaze events: %v", err)
	}

	st, stErr := store.New("pyestro-studio", store.Options{})
	if stErr != nil {
		log.Printf("Warning initializing local store: %v", stErr)
	}

	api := &AppAPI{
		webview: w,
		mgr:     mgr,
		events:  events,
		store:   st,
	}
	if _, err := glaze.BindMethods(w, "api", api); err != nil {
		log.Fatalf("glaze bind methods: %v", err)
	}

	m, err := menu.Set([]menu.Item{
		{Title: "Pyestro Studio", Submenu: []menu.Item{
			{Title: "About Pyestro Studio", OnClick: func() { _ = events.Emit("app:navigate", "about") }},
			{Separator: true},
			{Title: "Quit", Shortcut: "cmd+q", OnClick: w.Terminate},
		}},
		{Title: "File", Submenu: []menu.Item{
			{Title: "Open Flow...", Shortcut: "cmd+o", OnClick: func() { _ = events.Emit("app:action", "open-flow") }},
			{Title: "Open Project Folder...", Shortcut: "cmd+shift+o", OnClick: func() { _ = events.Emit("app:action", "open-project") }},
			{Separator: true},
			{Title: "Save Flow", Shortcut: "cmd+s", OnClick: func() { _ = events.Emit("app:action", "save-flow") }},
			{Title: "Save Flow As...", Shortcut: "cmd+shift+s", OnClick: func() { _ = events.Emit("app:action", "save-flow-as") }},
		}},
		{Title: "Edit", Submenu: []menu.Item{
			{Title: "Undo", Shortcut: "cmd+z", Selector: "undo:"},
			{Title: "Redo", Shortcut: "cmd+shift+z", Selector: "redo:"},
			{Separator: true},
			{Title: "Cut", Shortcut: "cmd+x", Selector: "cut:"},
			{Title: "Copy", Shortcut: "cmd+c", Selector: "copy:"},
			{Title: "Paste", Shortcut: "cmd+v", Selector: "paste:"},
			{Title: "Select All", Shortcut: "cmd+a", Selector: "selectAll:"},
		}},
		{Title: "Studio", Submenu: []menu.Item{
			{Title: "Refresh Snapshot", Shortcut: "cmd+r", OnClick: func() { _ = events.Emit("app:action", "refresh-snapshot") }},
			{Title: "Validate YAML", Shortcut: "cmd+shift+v", OnClick: func() { _ = events.Emit("app:action", "validate-flow") }},
			{Title: "Run Flow", Shortcut: "cmd+enter", OnClick: func() { _ = events.Emit("app:action", "run-flow") }},
		}},
		{Title: "Navigate", Submenu: []menu.Item{
			{Title: "Studio Inspector", Shortcut: "cmd+1", OnClick: func() { _ = events.Emit("app:navigate", "studio") }},
			{Title: "System & Environment", Shortcut: "cmd+2", OnClick: func() { _ = events.Emit("app:navigate", "system") }},
			{Title: "About", Shortcut: "cmd+3", OnClick: func() { _ = events.Emit("app:navigate", "about") }},
		}},
	}, menu.Options{Window: w.Window()})
	if err != nil && !errors.Is(err, menu.ErrUnsupported) {
		log.Printf("Warning installing native menu: %v", err)
	}
	if m != nil {
		defer m.Release()
	}

	w.SetTitle("Pyestro Studio — Maestro YAML Inspector & Runner")
	w.SetSize(1340, 840, glaze.HintNone)
	w.SetSize(1024, 768, glaze.HintMin)
	w.Navigate("app://home/index.html")

	log.Printf("Pyestro Studio ready (portless origin app://home/index.html)")
	w.Run()
	select {
	case <-done:
	default:
	}
	mgr.Shutdown()
}
