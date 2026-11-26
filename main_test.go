package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pyestro-studio/store"
	"pyestro-studio/studio"

	"github.com/crgimenes/glaze"
)

func TestAssetName(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"app://home/", "index.html"},
		{"app://home/index.html", "index.html"},
		{"app://home/app.css", "app.css"},
		{"app://home/app.js", "app.js"},
		{"app://home/icon.png", "icon.png"},
		{"app://home/../secret", "secret"},
	}
	for _, tc := range cases {
		if got := assetName(tc.url); got != tc.want {
			t.Errorf("assetName(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

func TestServeAsset(t *testing.T) {
	resp := serveAsset(&glaze.SchemeRequest{URL: "app://home/index.html"})
	if resp == nil || len(resp.Body) == 0 {
		t.Fatal("expected index.html response body")
	}
	if !strings.Contains(resp.MIMEType, "text/html") {
		t.Errorf("unexpected MIME type for index.html: %q", resp.MIMEType)
	}

	cssResp := serveAsset(&glaze.SchemeRequest{URL: "app://home/app.css"})
	if cssResp == nil || !strings.Contains(cssResp.MIMEType, "text/css") {
		t.Errorf("expected text/css MIME type, got %+v", cssResp)
	}

	iconResp := serveAsset(&glaze.SchemeRequest{URL: "app://home/icon.png"})
	if iconResp == nil || iconResp.MIMEType != "image/png" {
		t.Errorf("expected image/png MIME type, got %+v", iconResp)
	}

	missing := serveAsset(&glaze.SchemeRequest{URL: "app://home/nonexistent.js"})
	if missing != nil {
		t.Errorf("expected nil response for missing file with extension, got %+v", missing)
	}

	spaFallback := serveAsset(&glaze.SchemeRequest{URL: "app://home/studio"})
	if spaFallback == nil || !strings.Contains(spaFallback.MIMEType, "text/html") {
		t.Errorf("expected SPA fallback to index.html, got %+v", spaFallback)
	}
}

func TestAppAPISystemInfo(t *testing.T) {
	api := &AppAPI{
		mgr: studio.NewManager(nil),
	}

	info := api.SystemInfo()
	if info.OS == "" || info.Arch == "" {
		t.Fatalf("expected non-empty OS and Arch, got %+v", info)
	}
	if info.Pyestro.Name != "pyestro" || info.PyestroWeb.Name != "pyestro-web" {
		t.Fatalf("unexpected tool statuses: %+v / %+v", info.Pyestro, info.PyestroWeb)
	}
}

func TestSaveFlowFileOverwriteProtection(t *testing.T) {
	api := &AppAPI{}
	flowPath := filepath.Join(t.TempDir(), "smoke.yaml")
	initial := []byte("appId: com.example\n---\n- launchApp\n")
	if err := os.WriteFile(flowPath, initial, 0644); err != nil {
		t.Fatalf("write initial flow: %v", err)
	}

	// Refuses overwrite without ExpectedHash or Force.
	if _, err := api.SaveFlowFile(studio.SaveFlowRequest{
		Path:    flowPath,
		Content: "appId: com.changed\n",
	}); err == nil {
		t.Fatal("expected error when overwriting existing file without ExpectedHash or Force")
	}

	// Refuses overwrite on stale ExpectedHash.
	if _, err := api.SaveFlowFile(studio.SaveFlowRequest{
		Path:         flowPath,
		Content:      "appId: com.changed\n",
		ExpectedHash: "deadbeef",
	}); err == nil {
		t.Fatal("expected error when ExpectedHash does not match file on disk")
	}

	// Succeeds when ExpectedHash matches and returns updated Hash.
	resp, err := api.SaveFlowFile(studio.SaveFlowRequest{
		Path:         flowPath,
		Content:      "appId: com.updated\n",
		ExpectedHash: hashBytes(initial),
	})
	if err != nil {
		t.Fatalf("SaveFlowFile with matching hash failed: %v", err)
	}
	if resp.Hash == "" || resp.Hash == hashBytes(initial) {
		t.Fatalf("expected updated content hash, got %q", resp.Hash)
	}
}

func TestAppAPIEnvConfigPersistence(t *testing.T) {
	st, err := store.New("pyestro-studio-test", store.Options{
		Path: filepath.Join(t.TempDir(), "store.json"),
	})
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}

	api := &AppAPI{store: st}
	flowA := filepath.Join(t.TempDir(), "a.yaml")
	flowB := filepath.Join(t.TempDir(), "b.yaml")

	if err := api.SaveEnvConfig(studio.EnvConfig{
		FlowPath: flowA,
		EnvFile:  "/tmp/.env.a",
		RawEnv:   "USER=ana\n",
		Env:      map[string]string{"USER": "ana"},
	}); err != nil {
		t.Fatalf("SaveEnvConfig: %v", err)
	}

	gotA, err := api.GetEnvConfig(flowA)
	if err != nil {
		t.Fatalf("GetEnvConfig(flowA): %v", err)
	}
	if gotA.EnvFile != "/tmp/.env.a" || gotA.Env["USER"] != "ana" || gotA.RawEnv != "USER=ana\n" {
		t.Fatalf("unexpected env config for flowA: %+v", gotA)
	}

	gotB, err := api.GetEnvConfig(flowB)
	if err != nil {
		t.Fatalf("GetEnvConfig(flowB): %v", err)
	}
	if gotB.EnvFile != "" || len(gotB.Env) != 0 {
		t.Fatalf("expected isolated empty env config for flowB, got %+v", gotB)
	}
}

func TestScanProjectDirAndReadFlowFile(t *testing.T) {
	root := t.TempDir()
	maestroDir := filepath.Join(root, ".maestro")
	subMaestroDir := filepath.Join(maestroDir, "sub")
	gitDir := filepath.Join(root, ".git")
	nodeModules := filepath.Join(root, "node_modules", "pkg")

	for _, dir := range []string{subMaestroDir, gitDir, nodeModules} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	smokePath := filepath.Join(maestroDir, "smoke.yaml")
	subPath := filepath.Join(subMaestroDir, "login.yml")
	rootFlowPath := filepath.Join(root, "flow.yaml")
	envPath := filepath.Join(root, ".env")
	ignoredGit := filepath.Join(gitDir, "ignored.yaml")
	ignoredNode := filepath.Join(nodeModules, "ignored.yaml")

	files := map[string]string{
		smokePath:    "appId: com.example\n---\n- launchApp\n",
		subPath:      "appId: com.example\n---\n- tapOn: Login\n",
		rootFlowPath: "appId: com.example\n---\n- assertVisible: Home\n",
		envPath:      "EMAIL=user@example.com\n",
		ignoredGit:   "ignored: true\n",
		ignoredNode:  "ignored: true\n",
	}
	for p, content := range files {
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	api := &AppAPI{}
	scan, err := api.ScanProjectFolder(root)
	if err != nil {
		t.Fatalf("ScanProjectFolder: %v", err)
	}
	if len(scan.Flows) != 3 {
		t.Fatalf("expected 3 flows (.maestro + root), got %d: %+v", len(scan.Flows), scan.Flows)
	}
	if !scan.Flows[0].InDot || scan.Flows[0].RelPath != ".maestro/smoke.yaml" {
		t.Fatalf("expected .maestro/smoke.yaml first, got %+v", scan.Flows[0])
	}
	if !scan.Flows[1].InDot || scan.Flows[1].RelPath != ".maestro/sub/login.yml" {
		t.Fatalf("expected .maestro/sub/login.yml second, got %+v", scan.Flows[1])
	}
	if scan.Flows[2].InDot || scan.Flows[2].RelPath != "flow.yaml" {
		t.Fatalf("expected flow.yaml third, got %+v", scan.Flows[2])
	}
	if len(scan.EnvFiles) != 1 || scan.EnvFiles[0].RelPath != ".env" {
		t.Fatalf("expected [.env] in EnvFiles, got %+v", scan.EnvFiles)
	}

	loaded, err := api.ReadFlowFile(smokePath)
	if err != nil {
		t.Fatalf("ReadFlowFile: %v", err)
	}
	if loaded.Path != smokePath || loaded.Content != files[smokePath] || loaded.Hash == "" {
		t.Fatalf("unexpected ReadFlowFile response: %+v", loaded)
	}
}
