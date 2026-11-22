# Pyestro Studio

[![Go 1.27+](https://img.shields.io/badge/Go-1.27%2B-00ADD8.svg)](https://go.dev/)
[![License: GPL v3+](https://img.shields.io/badge/License-GPLv3%2B-blue.svg)](LICENSE)
[![Maestro Compatible](https://img.shields.io/badge/Maestro-compatible-success.svg)](https://docs.maestro.dev/)
![Platform: macOS | Linux | Windows](https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey.svg)

> **⚠️ THIS PROJECT IS CURRENTLY UNDER DEVELOPMENT.**

Desktop visual inspector, interactive step REPL, and Maestro-compatible YAML flow runner for [Pyestro](https://github.com/carlosxfelipe/pyestro) (Android via ADB) and [Pyestro Web](https://github.com/carlosxfelipe/pyestro-web) (Web & React Native for Web via Playwright).

Built in **pure Go (`CGO_ENABLED=0`)** using [Glaze](https://github.com/crgimenes/glaze) (`WKWebView` on macOS, `WebKitGTK` on Linux, `WebView2` on Windows) paired with an embedded Python JSON-RPC sidecar (`studio/worker.py`).

## Features

- **Interactive Screen Inspector:** Capture live screenshots and clickable element overlays (`uiautomator dump` on Android, DOM extraction on Web) to inspect bounds, `id`, `text`, `content-desc`, and copy ranked Maestro YAML selectors.
- **Step REPL & Flow Editor:** Execute single Maestro YAML commands (`tapOn`, `inputText`, `assertVisible`, etc.) against an active session without restarting, edit `.yaml` flows, validate syntax with the real Python parser, and run full flows with optional video recording and HTML/JUnit reports.
- **Dual Target Support:** Switch seamlessly between **Android (`pyestro`)** and **Web (`pyestro-web`)** from the same Studio window.
- **Environment Diagnostics:** Automatically detects `uv`, `adb`, `pyestro`, and `pyestro-web` (from `PATH`, standard `bin` / `uv tool` directories, and macOS/Linux Android SDK locations) and lets you install or upgrade `uv`, `pyestro`, and `pyestro-web` from the **Environment** tab (with guided setup instructions for `adb`).

## Setup & Requirements

Pyestro Studio is the desktop interface for running your flows. Before running tests, you need **`uv`** installed on your computer, along with **`pyestro`** (for Android) and/or **`pyestro-web`** (for Web).

> **Tip:** You can install `uv`, `pyestro`, and `pyestro-web` directly from the **Environment** tab inside Pyestro Studio, or follow the step-by-step commands below.

### 1. Install `uv` (Required)

**macOS:**

```bash
curl -LsSf https://astral.sh/uv/install.sh | sh
```

_(Or `brew install uv` if you already use Homebrew.)_

**Linux:**

```bash
curl -LsSf https://astral.sh/uv/install.sh | sh
```

**Windows (PowerShell):**

```powershell
powershell -ExecutionPolicy ByPass -c "irm https://astral.sh/uv/install.ps1 | iex"
```

> If your terminal says `uv` is not found after installing, close and reopen the terminal window.

### 2. Install `pyestro` & `adb` (Only for Android)

Install `pyestro`:

```bash
uv tool install git+https://github.com/carlosxfelipe/pyestro.git
```

Install Android Platform Tools (`adb`):

- **macOS:** `brew install android-platform-tools` (or install Android Studio)
- **Linux (Ubuntu/Debian):** `sudo apt install adb`
- **Windows:** Download [Android SDK Platform Tools](https://developer.android.com/tools/releases/platform-tools), unzip it, and add the `platform-tools` folder to your system `PATH` (or install Android Studio).

On physical Android phones, enable **USB debugging** under **Settings → About phone → tap Build number several times → Developer options**, connect via USB, and accept the prompt on the phone.

### 3. Install `pyestro-web` (Only for Web)

Install `pyestro-web`:

```bash
uv tool install git+https://github.com/carlosxfelipe/pyestro-web.git
```

Install the Chromium browser used by Playwright:

```bash
uv tool run --from pyestro-web playwright install chromium
```

_(Optional: if you want to select **Firefox** or **WebKit** in the Inspector, also run `uv tool run --from pyestro-web playwright install firefox webkit`.)_

## Running Pyestro Studio

Run the desktop application directly with Go:

```bash
go run main.go
```

If you want to package a standalone desktop binary, use the included Go build scripts (`go run build_mac.go` for a macOS `.app` bundle or `go run build_linux.go` for a Linux `.AppImage`).

## Quick Start Example

Both **Web** and **Android** can run the same [expo-route-kit](https://github.com/carlosxfelipe/expo-route-kit) smoke flow below.

### Web (`pyestro-web`)

Test right away using the hosted demo app at `https://expo-route-kit.netlify.app/`:

1. Open the **Inspector** tab and select **Web (`pyestro-web`)**.
2. Enter `https://expo-route-kit.netlify.app/` in the URL input (which defaults to `http://localhost:8081`).
3. Click **Connect** to capture the live screen and inspect elements, or paste the flow below into the **Maestro YAML Flow** editor and click **Validate** / **Run Flow**.

### Android (`pyestro`)

1. Start an Android emulator (via Android Studio's **Device Manager** or `emulator -avd <name>`) or connect a physical phone with **USB debugging** enabled, and confirm `adb devices` lists it as `device`.
2. Clone `expo-route-kit` and install the native Android build (Expo Go will not match `appId`):

   ```bash
   git clone https://github.com/carlosxfelipe/expo-route-kit.git
   cd expo-route-kit && npm install
   npm run build:android && adb install -r apk/expo-route-kit-v1.0.0.apk
   ```

3. Open the **Inspector** tab, select **Android (`pyestro`)**, optionally enter `com.carlosxfelipe.exporoutekit` in **Optional appId**, and click **Connect** (click **Re-scan Tools** if you started the emulator after opening Studio).
4. Open `.maestro/smoke.yaml` from the cloned repo (or paste the flow below) into **Maestro YAML Flow** and click **Validate** / **Run Flow**.

### Sample Flow (`expo-route-kit.yaml`)

```yaml
appId: com.carlosxfelipe.exporoutekit
name: Smoke - Login and Navigation
tags:
  - smoke
env:
  EMAIL: user@example.com
  PASS: "123456"
---
- launchApp
- extendedWaitUntil:
    visible:
      id: input_login_email
    timeout: 15000
- assertVisible: "Welcome back"
- tapOn:
    id: input_login_email
- inputText: ${EMAIL}
- tapOn:
    id: input_login_password
- inputText: ${PASS}
- hideKeyboard
- tapOn:
    id: btn_login_sign_in
- extendedWaitUntil:
    visible: "Welcome to Expo Route Kit"
    timeout: 5000
- tapOn:
    id: tab_explore
- assertVisible: "AppButton Variants"
- tapOn:
    id: tab_profile
- assertVisible: "user@example.com"
- tapOn:
    id: btn_profile_sign_out
- extendedWaitUntil:
    visible:
      id: btn_profile_sign_out_confirm
    timeout: 5000
- tapOn:
    id: btn_profile_sign_out_confirm
- extendedWaitUntil:
    visible:
      id: btn_login_sign_in
    timeout: 5000
```

## Commands

### Run & Validate

```bash
go mod tidy
go vet ./...
go test ./...
go run .
```

## License

This project is licensed under the GNU General Public License v3.0 or later ([GPL-3.0-or-later](LICENSE)).
