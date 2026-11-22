package studio

import (
	"bufio"
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const packagingExplanation = "To run tests, make sure uv is installed on your computer (and adb if you are testing Android devices). Once uv is available, you can install pyestro and pyestro-web using the buttons below."

// LocateBinary searches PATH and common macOS/Linux install locations for an executable.
func LocateBinary(name string, extraDirs ...string) (string, string) {
	if p, err := exec.LookPath(name); err == nil {
		return p, "PATH"
	}

	home, _ := os.UserHomeDir()
	searchDirs := make([]string, 0, 8+len(extraDirs))
	if home != "" {
		searchDirs = append(searchDirs,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".cargo", "bin"),
		)
	}
	searchDirs = append(searchDirs,
		"/opt/homebrew/bin",
		"/usr/local/bin",
		"/usr/bin",
		"/bin",
	)
	searchDirs = append(searchDirs, extraDirs...)

	for _, dir := range searchDirs {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate, "Standard bin"
		}
		if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
			exeCandidate := candidate + ".exe"
			if fi, err := os.Stat(exeCandidate); err == nil && !fi.IsDir() {
				return exeCandidate, "Standard bin"
			}
		}
	}

	return "", "Missing"
}

// ParseShebangPython inspects an executable script's #! line to extract its Python interpreter.
func ParseShebangPython(scriptPath string) string {
	f, err := os.Open(scriptPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return ""
	}
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), "#!")
	if !ok {
		return ""
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return ""
	}
	if filepath.Base(fields[0]) == "env" && len(fields) > 1 {
		if p, err := exec.LookPath(fields[1]); err == nil {
			return p
		}
		return fields[1]
	}
	if fi, err := os.Stat(fields[0]); err == nil && !fi.IsDir() {
		return fields[0]
	}
	return ""
}

// ResolvePyestroTool locates an installed pyestro or pyestro-web package and its backing Python interpreter.
func ResolvePyestroTool(toolName string, uvPath string) ToolStatus {
	moduleName := "pyestro"
	if toolName == "pyestro-web" {
		moduleName = "pyestro_web"
	}

	status := ToolStatus{
		Name: toolName,
	}

	if binPath, src := LocateBinary(toolName); binPath != "" {
		py := ParseShebangPython(binPath)
		if py == "" {
			py = cmp.Or(findSiblingVenvPython(binPath), "")
		}
		if py != "" {
			if ok, ver := checkModuleVersion(py, moduleName); ok {
				status.Found = true
				status.Path = binPath
				status.PythonPath = py
				status.Version = ver
				if strings.Contains(py, filepath.Join("uv", "tools")) {
					status.Source = "uv tool"
				} else {
					status.Source = src
				}
				return status
			}
		}
	}

	// Check uv tool directory directly in case ~/.local/bin isn't populated.
	if uvToolDir := resolveUVToolDir(); uvToolDir != "" {
		uvPython := filepath.Join(uvToolDir, toolName, "bin", "python")
		if ok, ver := checkModuleVersion(uvPython, moduleName); ok {
			status.Found = true
			status.Path = filepath.Join(uvToolDir, toolName, "bin", toolName)
			status.PythonPath = uvPython
			status.Version = ver
			status.Source = "uv tool dir"
			return status
		}
	}

	if uvPath != "" {
		status.Hint = "Click Install / Upgrade under Install & Manage Tools below."
	} else {
		status.Hint = "Click Install uv under Install & Manage Tools below first."
	}
	status.Source = "Missing"
	return status
}

func resolveUVToolDir() string {
	if custom := strings.TrimSpace(os.Getenv("UV_TOOL_DIR")); custom != "" {
		return custom
	}
	if xdgData := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdgData != "" {
		return filepath.Join(xdgData, "uv", "tools")
	}
	if home, _ := os.UserHomeDir(); home != "" {
		return filepath.Join(home, ".local", "share", "uv", "tools")
	}
	return ""
}

func findSiblingVenvPython(binPath string) string {
	candidate := filepath.Join(filepath.Dir(binPath), "python")
	if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
		return candidate
	}
	return ""
}

func checkModuleVersion(pythonPath string, moduleName string) (bool, string) {
	if pythonPath == "" {
		return false, ""
	}
	if fi, err := os.Stat(pythonPath); err != nil || fi.IsDir() {
		return false, ""
	}
	script := fmt.Sprintf("import importlib.metadata as m, %s; print(getattr(%s, '__version__', '') or 'installed')", moduleName, moduleName)
	out, err := exec.Command(pythonPath, "-c", script).Output()
	if err != nil {
		return false, ""
	}
	return true, strings.TrimSpace(string(out))
}

func firstLineOutput(binPath string, args ...string) string {
	out, err := exec.Command(binPath, args...).Output()
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(out), "\n")
	return strings.TrimSpace(line)
}

func adbExtraDirs() []string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, "Library", "Android", "sdk", "platform-tools"),
		filepath.Join(home, "Android", "Sdk", "platform-tools"),
	}
}

// GetSystemInfo inspects host tools, uv/adb/pyestro/pyestro-web availability, and ADB devices.
func GetSystemInfo() SystemInfo {
	uvPath, uvSource := LocateBinary("uv")
	uvStatus := ToolStatus{
		Name:   "uv",
		Found:  uvPath != "",
		Path:   uvPath,
		Source: uvSource,
	}
	if uvPath != "" {
		uvStatus.Version = firstLineOutput(uvPath, "--version")
	} else {
		uvStatus.Hint = "Install uv from https://docs.astral.sh/uv/ to manage pyestro & pyestro-web."
	}

	adbPath, adbSource := LocateBinary("adb", adbExtraDirs()...)
	adbStatus := ToolStatus{
		Name:   "adb",
		Found:  adbPath != "",
		Path:   adbPath,
		Source: adbSource,
	}
	if adbPath != "" {
		adbStatus.Version = firstLineOutput(adbPath, "version")
	} else {
		adbStatus.Hint = "Install Android SDK Platform Tools (adb) and ensure it is on PATH."
	}

	pyestroStatus := ResolvePyestroTool("pyestro", uvPath)
	pyestroWebStatus := ResolvePyestroTool("pyestro-web", uvPath)

	devices := ListAndroidDevices(adbPath)

	return SystemInfo{
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		PackagingNote: packagingExplanation,
		UV:            uvStatus,
		ADB:           adbStatus,
		Pyestro:       pyestroStatus,
		PyestroWeb:    pyestroWebStatus,
		AndroidReady:  pyestroStatus.Found && adbStatus.Found,
		WebReady:      pyestroWebStatus.Found,
		Devices:       devices,
	}
}

// ListAndroidDevices queries `adb devices` when adb is available.
func ListAndroidDevices(adbPath string) []AndroidDevice {
	if adbPath == "" {
		return []AndroidDevice{}
	}
	out, err := exec.Command(adbPath, "devices").Output()
	if err != nil {
		return []AndroidDevice{}
	}
	devices := []AndroidDevice{}
	for line := range strings.SplitSeq(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "List of devices") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) >= 2 {
			devices = append(devices, AndroidDevice{
				Serial: fields[0],
				State:  fields[1],
			})
		}
	}
	return devices
}
