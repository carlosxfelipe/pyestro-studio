package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
