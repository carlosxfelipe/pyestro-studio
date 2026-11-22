package studio

import (
	"os"
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
