package studio

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseShebangPython(t *testing.T) {
	tmpDir := t.TempDir()
	fakePy := filepath.Join(tmpDir, "python3")
	if err := os.WriteFile(fakePy, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write fake python: %v", err)
	}

	script := filepath.Join(tmpDir, "pyestro")
	content := "#!" + fakePy + "\nimport sys\n"
	if err := os.WriteFile(script, []byte(content), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := ParseShebangPython(script)
	if got != fakePy {
		t.Fatalf("ParseShebangPython = %q, want %q", got, fakePy)
	}
}

func TestGetSystemInfoPackagingNoteAndFields(t *testing.T) {
	info := GetSystemInfo()
	if info.OS == "" || info.Arch == "" {
		t.Fatalf("expected non-empty OS and Arch, got %+v", info)
	}
	if !strings.Contains(info.PackagingNote, "uv") || !strings.Contains(info.PackagingNote, "adb") {
		t.Fatalf("expected packaging note to mention uv and adb, got %q", info.PackagingNote)
	}
	if info.Pyestro.Name != "pyestro" || info.PyestroWeb.Name != "pyestro-web" {
		t.Fatalf("unexpected tool names: %+v / %+v", info.Pyestro, info.PyestroWeb)
	}
	if info.Pyestro.Source == "Local .venv" || info.PyestroWeb.Source == "Local .venv" {
		t.Fatalf("expected local .venv detection to be disabled, got %+v / %+v", info.Pyestro, info.PyestroWeb)
	}
}

func TestResolvePyestroToolIgnoresLocalVenv(t *testing.T) {
	tmpRoot := t.TempDir()
	studioDir := filepath.Join(tmpRoot, "pyestro-studio")
	fakePyDir := filepath.Join(tmpRoot, "pyestro", ".venv", "bin")
	if err := os.MkdirAll(studioDir, 0755); err != nil {
		t.Fatalf("mkdir studioDir: %v", err)
	}
	if err := os.MkdirAll(fakePyDir, 0755); err != nil {
		t.Fatalf("mkdir fakePyDir: %v", err)
	}
	fakePy := filepath.Join(fakePyDir, "python")
	if err := os.WriteFile(fakePy, []byte("#!/bin/sh\necho 0.1.0\n"), 0755); err != nil {
		t.Fatalf("write fake python: %v", err)
	}

	t.Setenv("UV_TOOL_DIR", filepath.Join(tmpRoot, "empty-uv-tools"))
	t.Chdir(studioDir)

	status := ResolvePyestroTool("pyestro", "")
	if status.Source == "Local .venv" {
		t.Fatalf("expected ResolvePyestroTool not to use Local .venv, got %+v", status)
	}
}

func TestWorkerScriptEmbedded(t *testing.T) {
	if !strings.Contains(embeddedWorkerPy, "class StudioSession") {
		t.Fatal("expected embedded worker.py to contain StudioSession")
	}
}

func TestUVInstallCommand(t *testing.T) {
	bin, args, display, err := UVInstallCommand("darwin", func(name string) bool { return name == "curl" })
	if err != nil || bin != "sh" || len(args) != 2 || !strings.Contains(display, "install.sh") {
		t.Fatalf("unexpected darwin curl result: bin=%q args=%v display=%q err=%v", bin, args, display, err)
	}

	bin, args, display, err = UVInstallCommand("linux", func(name string) bool { return name == "wget" })
	if err != nil || bin != "sh" || len(args) != 2 || !strings.Contains(display, "wget") {
		t.Fatalf("unexpected linux wget result: bin=%q args=%v display=%q err=%v", bin, args, display, err)
	}

	bin, args, display, err = UVInstallCommand("windows", func(name string) bool { return name == "powershell" })
	if err != nil || bin != "powershell" || len(args) == 0 || !strings.Contains(display, "install.ps1") {
		t.Fatalf("unexpected windows powershell result: bin=%q args=%v display=%q err=%v", bin, args, display, err)
	}

	if _, _, _, err := UVInstallCommand("plan9", func(string) bool { return true }); err == nil {
		t.Fatal("expected error for unsupported OS")
	}
}

func TestManagerValidateYAMLWithInstalledTools(t *testing.T) {
	info := GetSystemInfo()
	if !info.Pyestro.Found || !info.PyestroWeb.Found {
		t.Skip("installed pyestro and pyestro-web packages not available")
	}

	mgr := NewManager(nil)
	defer mgr.Shutdown()

	validResp, err := mgr.ValidateYAML(ValidateRequest{
		Target: "android",
		YAML:   "appId: com.android.settings\n---\n- launchApp\n- assertVisible: \"Display\"\n",
	})
	if err != nil {
		t.Fatalf("ValidateYAML android error: %v", err)
	}
	if !validResp.Valid || validResp.Steps != 2 {
		t.Fatalf("expected valid 2-step android flow, got %+v", validResp)
	}

	invalidResp, err := mgr.ValidateYAML(ValidateRequest{
		Target: "web",
		YAML:   "appId: com.example\n---\n- unknownFakeCommand: foo\n",
	})
	if err != nil {
		t.Fatalf("ValidateYAML web error: %v", err)
	}
	if invalidResp.Valid || invalidResp.Error == "" {
		t.Fatalf("expected invalid flow error for unknownFakeCommand, got %+v", invalidResp)
	}
}

func TestManagerValidateYAMLWithDotenvAndEnvOverrides(t *testing.T) {
	info := GetSystemInfo()
	if !info.Pyestro.Found {
		t.Skip("installed pyestro package not available")
	}

	tmpDir := t.TempDir()
	otherCWD := filepath.Join(tmpDir, "elsewhere")
	if err := os.MkdirAll(otherCWD, 0755); err != nil {
		t.Fatalf("mkdir otherCWD: %v", err)
	}
	t.Chdir(otherCWD)

	projectDir := filepath.Join(tmpDir, "project")
	flowDir := filepath.Join(projectDir, ".maestro")
	if err := os.MkdirAll(flowDir, 0755); err != nil {
		t.Fatalf("mkdir flowDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("DOTENV_USER=ana\n"), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	stagingEnv := filepath.Join(projectDir, ".env.staging")
	if err := os.WriteFile(stagingEnv, []byte("STAGING_USER=bia\n"), 0644); err != nil {
		t.Fatalf("write .env.staging: %v", err)
	}
	flowPath := filepath.Join(flowDir, "smoke.yaml")

	mgr := NewManager(nil)
	defer mgr.Shutdown()

	// Auto-discovers .env from flowPath ancestors even when Studio CWD is elsewhere.
	resp, err := mgr.ValidateYAML(ValidateRequest{
		Target:   "android",
		FlowPath: flowPath,
		YAML:     "appId: com.example\n---\n- inputText: ${DOTENV_USER}\n",
	})
	if err != nil {
		t.Fatalf("ValidateYAML dotenv error: %v", err)
	}
	if !resp.Valid {
		t.Fatalf("expected valid flow with auto-discovered .env, got %+v", resp)
	}

	// Supports explicit envFile and env overrides.
	resp, err = mgr.ValidateYAML(ValidateRequest{
		Target:   "android",
		FlowPath: flowPath,
		EnvFile:  stagingEnv,
		Env:      map[string]string{"CLI_TOKEN": "xyz"},
		YAML:     "appId: com.example\n---\n- inputText: ${STAGING_USER}:${CLI_TOKEN}\n",
	})
	if err != nil {
		t.Fatalf("ValidateYAML envFile+env error: %v", err)
	}
	if !resp.Valid {
		t.Fatalf("expected valid flow with envFile and env overrides, got %+v", resp)
	}

	// Missing explicit envFile fails validation cleanly.
	missingResp, err := mgr.ValidateYAML(ValidateRequest{
		Target:  "android",
		EnvFile: filepath.Join(projectDir, "missing.env"),
		YAML:    "appId: com.example\n---\n- launchApp\n",
	})
	if err != nil {
		t.Fatalf("ValidateYAML missing envFile returned transport error: %v", err)
	}
	if missingResp.Valid || !strings.Contains(missingResp.Error, "Env file not found") {
		t.Fatalf("expected invalid flow for missing envFile, got %+v", missingResp)
	}
}

func TestWorkerBuildStepEnv(t *testing.T) {
	info := GetSystemInfo()
	if !info.Pyestro.Found || info.Pyestro.PythonPath == "" {
		t.Skip("installed pyestro python not available")
	}

	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "app")
	flowDir := filepath.Join(projectDir, "flows")
	if err := os.MkdirAll(flowDir, 0755); err != nil {
		t.Fatalf("mkdir flowDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("A=dotenv\nB=dotenv\nC=dotenv\n"), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	workerFile := filepath.Join(tmpDir, "worker.py")
	if err := os.WriteFile(workerFile, []byte(embeddedWorkerPy), 0644); err != nil {
		t.Fatalf("write worker.py: %v", err)
	}

	script := `
import importlib.util, json, sys
spec = importlib.util.spec_from_file_location("studio_worker", sys.argv[1])
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)
s = mod.StudioSession("android")
s.runner = type("R", (), {"_env": {"SESSION": "1"}})()
env = s._build_step_env({
    "flowPath": sys.argv[2],
    "flowYaml": "appId: a.b\nenv:\n  B: header\n  C: header\n---\n- launchApp\n",
    "env": {"C": "cli"},
})
print(json.dumps(env, sort_keys=True))
`
	out, err := exec.Command(info.Pyestro.PythonPath, "-c", script, workerFile, filepath.Join(flowDir, "login.yaml")).CombinedOutput()
	if err != nil {
		t.Fatalf("python _build_step_env failed: %v\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{`"A": "dotenv"`, `"B": "header"`, `"C": "cli"`, `"SESSION": "1"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %s in step env, got %s", want, got)
		}
	}
}

func TestWorkerDefaultArtifactsDirWritable(t *testing.T) {
	info := GetSystemInfo()
	if !info.Pyestro.Found || info.Pyestro.PythonPath == "" {
		t.Skip("installed pyestro python not available")
	}

	tmpDir := t.TempDir()
	fakeHome := filepath.Join(tmpDir, "home")
	if err := os.MkdirAll(filepath.Join(fakeHome, "Documents"), 0755); err != nil {
		t.Fatalf("mkdir fake Documents: %v", err)
	}
	t.Setenv("HOME", fakeHome)

	workerFile := filepath.Join(tmpDir, "worker.py")
	if err := os.WriteFile(workerFile, []byte(embeddedWorkerPy), 0644); err != nil {
		t.Fatalf("write worker.py: %v", err)
	}

	script := `
import importlib.util, os, sys
from pathlib import Path
spec = importlib.util.spec_from_file_location("studio_worker", sys.argv[1])
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)
os.chdir("/")
p = mod._default_artifacts_dir()
want = Path(os.environ["HOME"]) / "Documents" / "Pyestro Studio" / "artifacts"
assert p == want, f"got {p}, want {want}"
print(p)
`
	out, err := exec.Command(info.Pyestro.PythonPath, "-c", script, workerFile).CombinedOutput()
	if err != nil {
		t.Fatalf("python _default_artifacts_dir failed: %v\n%s", err, out)
	}
}
