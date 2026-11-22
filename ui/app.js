const DEFAULT_ANDROID_YAML = `appId: com.android.settings
name: Settings smoke test
tags: [smoke]
---
- launchApp
- assertVisible: ".*"
`;

const DEFAULT_WEB_YAML = `appId: com.example.web
url: http://localhost:8081
name: Web smoke test
tags: [smoke]
---
- launchApp
- assertVisible: ".*"
`;

let systemInfo = null;
let currentTarget = "android";
let currentFlowPath = "";
let currentFlowHash = "";
let lastSavedContent = DEFAULT_ANDROID_YAML;
let currentSnapshot = null;
let selectedElement = null;
let sessionConnected = false;
let toastTimer = null;
let currentEnvConfig = { envFile: "", rawEnv: "", env: {} };

function parseEnvOverrides(rawText) {
  const result = {};
  for (const rawLine of String(rawText || "").split(/\r?\n/)) {
    let line = rawLine.trim();
    if (!line || line.startsWith("#")) continue;
    if (/^export\s+/.test(line)) {
      line = line.replace(/^export\s+/, "");
    }
    const eqIdx = line.indexOf("=");
    if (eqIdx <= 0) continue;
    const key = line.slice(0, eqIdx).trim();
    if (!key) continue;
    let val = line.slice(eqIdx + 1).trim();
    if (
      val.length >= 2 &&
      ((val.startsWith('"') && val.endsWith('"')) ||
        (val.startsWith("'") && val.endsWith("'")))
    ) {
      val = val.slice(1, -1);
    }
    result[key] = val;
  }
  return result;
}

function updateEnvPill() {
  const count = Object.keys(currentEnvConfig.env || {}).length;
  const hasFile = Boolean((currentEnvConfig.envFile || "").trim());
  const parts = [];
  if (hasFile) parts.push(fileBaseName(currentEnvConfig.envFile));
  if (count > 0) parts.push(`${count} ${count === 1 ? "var" : "vars"}`);
  if (parts.length === 0) {
    setPill("env-config-pill", "muted", "Auto");
  } else {
    setPill("env-config-pill", "ok", parts.join(" · "));
  }
}

async function loadEnvConfigForCurrentFlow() {
  if (!window.api_get_env_config) return;
  try {
    const cfg = await window.api_get_env_config(currentFlowPath || "");
    const rawEnv = (cfg && cfg.rawEnv) || "";
    const envFile = (cfg && cfg.envFile) || "";
    const env = (cfg && cfg.env) || parseEnvOverrides(rawEnv);
    currentEnvConfig = { envFile, rawEnv, env };
    updateEnvPill();
  } catch (_) {}
}

let envEscHandler = null;

function openEnvDialog() {
  const backdrop = document.getElementById("env-dialog-backdrop");
  const fileInput = document.getElementById("env-file-input");
  const varsInput = document.getElementById("env-vars-input");
  if (!backdrop || !fileInput || !varsInput) return;

  fileInput.value = currentEnvConfig.envFile || "";
  varsInput.value = currentEnvConfig.rawEnv || "";
  backdrop.classList.remove("hidden");

  if (envEscHandler) window.removeEventListener("keydown", envEscHandler);
  envEscHandler = (e) => {
    if (e.key === "Escape") {
      e.preventDefault();
      closeEnvDialog(false);
    }
  };
  window.addEventListener("keydown", envEscHandler);
  fileInput.focus();
}

async function browseEnvFile() {
  try {
    const res = await window.api_open_env_file();
    if (res && res.path) {
      document.getElementById("env-file-input").value = res.path;
    }
  } catch (err) {
    showToast("Failed to select .env file: " + err);
  }
}

function clearEnvFileInput() {
  const input = document.getElementById("env-file-input");
  if (input) input.value = "";
}

async function closeEnvDialog(save) {
  const backdrop = document.getElementById("env-dialog-backdrop");
  if (backdrop) backdrop.classList.add("hidden");
  if (envEscHandler) {
    window.removeEventListener("keydown", envEscHandler);
    envEscHandler = null;
  }
  if (!save) return;

  const envFile = (
    document.getElementById("env-file-input").value || ""
  ).trim();
  const rawEnv = document.getElementById("env-vars-input").value || "";
  const env = parseEnvOverrides(rawEnv);
  currentEnvConfig = { envFile, rawEnv, env };
  updateEnvPill();

  if (window.api_save_env_config) {
    try {
      await window.api_save_env_config({
        flowPath: currentFlowPath || "",
        envFile,
        rawEnv,
        env,
      });
    } catch (err) {
      showToast("Failed to save environment settings: " + err);
      return;
    }
  }
  showToast("Environment settings updated");
}

function showToast(msg) {
  const el = document.getElementById("adw-toast");
  el.textContent = msg;
  el.classList.add("visible");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => el.classList.remove("visible"), 3000);
}

function escapeHTML(str) {
  return String(str || "").replace(
    /[&<>"']/g,
    (s) =>
      ({
        "&": "&amp;",
        "<": "&lt;",
        ">": "&gt;",
        '"': "&quot;",
        "'": "&#39;",
      })[s],
  );
}

function appendLog(line) {
  const pre = document.getElementById("console-output");
  const ts = new Date().toLocaleTimeString([], { hour12: false });
  pre.textContent += `[${ts}] ${line}\n`;
  pre.scrollTop = pre.scrollHeight;
}

function switchPage(page) {
  ["studio", "system", "about"].forEach((p) => {
    const pageEl = document.getElementById("page-" + p);
    const navEl = document.getElementById("nav-" + p);
    if (pageEl) pageEl.classList.toggle("hidden", p !== page);
    if (navEl) {
      const isCurrent = p === page;
      navEl.classList.toggle("active", isCurrent);
      if (isCurrent) {
        navEl.setAttribute("aria-current", "page");
      } else {
        navEl.removeAttribute("aria-current");
      }
    }
  });
  if (page === "studio") {
    requestAnimationFrame(fitScreenFrame);
  }
}

function switchBottomTab(tab) {
  ["logs", "tree"].forEach((t) => {
    const btn = document.getElementById("tab-btn-" + t);
    btn.classList.toggle("active", t === tab);
    btn.setAttribute("aria-selected", t === tab ? "true" : "false");
    document
      .getElementById("tab-panel-" + t)
      .classList.toggle("hidden", t !== tab);
  });
}

function selectTarget(target) {
  currentTarget = target === "web" ? "web" : "android";
  const androidBtn = document.getElementById("target-btn-android");
  const webBtn = document.getElementById("target-btn-web");
  androidBtn.classList.toggle("active", currentTarget === "android");
  androidBtn.setAttribute(
    "aria-selected",
    currentTarget === "android" ? "true" : "false",
  );
  webBtn.classList.toggle("active", currentTarget === "web");
  webBtn.setAttribute(
    "aria-selected",
    currentTarget === "web" ? "true" : "false",
  );
  document
    .getElementById("conn-fields-android")
    .classList.toggle("hidden", currentTarget !== "android");
  document
    .getElementById("conn-fields-web")
    .classList.toggle("hidden", currentTarget !== "web");
  const kbdBtn = document.getElementById("btn-quick-kbd");
  if (kbdBtn) {
    kbdBtn.classList.toggle("hidden", currentTarget === "web");
  }

  const editor = document.getElementById("yaml-editor");
  if (!currentFlowPath) {
    if (
      currentTarget === "web" &&
      editor.value.trim() === DEFAULT_ANDROID_YAML.trim()
    ) {
      editor.value = DEFAULT_WEB_YAML;
      lastSavedContent = DEFAULT_WEB_YAML;
    } else if (
      currentTarget === "android" &&
      editor.value.trim() === DEFAULT_WEB_YAML.trim()
    ) {
      editor.value = DEFAULT_ANDROID_YAML;
      lastSavedContent = DEFAULT_ANDROID_YAML;
    }
    updateFlowStateUI();
  }
  updateStudioEnvBanner();
}

function setPill(id, state, text) {
  const el = document.getElementById(id);
  if (!el) return;
  el.className = "status-pill " + state;
  el.textContent = text;
}

function updateStudioEnvBanner() {
  const banner = document.getElementById("studio-env-banner");
  if (!systemInfo) {
    banner.classList.add("hidden");
    return;
  }

  if (currentTarget === "android" && !systemInfo.androidReady) {
    const missing = [];
    if (!systemInfo.pyestro.found) missing.push("pyestro");
    if (!systemInfo.adb.found) missing.push("adb");
    document.getElementById("studio-env-banner-title").textContent =
      `Android target requires: ${missing.join(" and ")}`;
    document.getElementById("studio-env-banner-msg").textContent =
      "Make sure adb and pyestro are installed in the Environment tab.";
    banner.classList.remove("hidden");
    return;
  }

  if (currentTarget === "web" && !systemInfo.webReady) {
    document.getElementById("studio-env-banner-title").textContent =
      "Web target requires: pyestro-web";
    document.getElementById("studio-env-banner-msg").textContent =
      "Install pyestro-web in the Environment tab.";
    banner.classList.remove("hidden");
    return;
  }

  banner.classList.add("hidden");
}

function formatOSName(os) {
  if (os === "darwin") return "macOS";
  if (os === "linux") return "Linux";
  if (os === "windows") return "Windows";
  return os || "Unknown OS";
}

function renderSystemInfo(info) {
  systemInfo = info;

  setPill(
    "diag-android-ready",
    info.androidReady ? "ok" : "warn",
    info.androidReady ? "Ready" : "Missing tool(s)",
  );
  setPill(
    "diag-web-ready",
    info.webReady ? "ok" : "warn",
    info.webReady ? "Ready" : "Missing tool",
  );

  const dot = document.getElementById("env-status-dot");
  dot.className =
    "adw-nav-dot " + (info.androidReady || info.webReady ? "ok" : "warn");

  if (info.packagingNote) {
    document.getElementById("packaging-note-text").textContent =
      info.packagingNote;
  }

  const osLabel = `${formatOSName(info.os)} (${info.arch})`;
  const uvInstallDesc = document.getElementById("uv-install-desc");
  const uvBtn = document.getElementById("btn-install-uv");
  const pyestroBtn = document.getElementById("btn-install-pyestro");
  const pyestroWebBtn = document.getElementById("btn-install-pyestro-web");
  if (uvInstallDesc && uvBtn) {
    if (info.uv && info.uv.found) {
      uvInstallDesc.textContent = `Detected ${osLabel} — installed at ${info.uv.path}`;
      uvBtn.textContent = "Reinstall / Update";
      uvBtn.className = "adw-btn";
    } else {
      uvInstallDesc.textContent = `Detected ${osLabel} — required before installing pyestro or pyestro-web`;
      uvBtn.textContent = "Install uv";
      uvBtn.className = "adw-btn adw-btn-primary";
    }
  }
  if (pyestroBtn) {
    pyestroBtn.disabled = !(info.uv && info.uv.found);
    pyestroBtn.title = info.uv && info.uv.found ? "" : "Install uv first";
  }
  if (pyestroWebBtn) {
    pyestroWebBtn.disabled = !(info.uv && info.uv.found);
    pyestroWebBtn.title = info.uv && info.uv.found ? "" : "Install uv first";
  }

  const adbFound = !!(info.adb && info.adb.found);
  setPill(
    "adb-setup-status-pill",
    adbFound ? "ok" : "warn",
    adbFound ? "ADB Ready" : "ADB Missing",
  );
  const adbHintEl = document.getElementById("adb-setup-os-hint");
  if (adbHintEl) {
    if (adbFound) {
      adbHintEl.textContent = `ADB detected at ${info.adb.path}. Connect a phone or start an emulator and click Refresh.`;
    } else if (info.os === "darwin") {
      adbHintEl.innerHTML =
        "On macOS: run <code>brew install android-platform-tools</code> in Terminal, or install Android Studio (Studio also detects <code>~/Library/Android/sdk/platform-tools</code> automatically).";
    } else if (info.os === "linux") {
      adbHintEl.innerHTML =
        "On Linux: install via your package manager (e.g. <code>sudo apt install adb</code>), or unzip Platform Tools into <code>~/Android/Sdk/platform-tools</code>.";
    } else if (info.os === "windows") {
      adbHintEl.innerHTML =
        "On Windows: download Platform Tools below, unzip it (or install Android Studio), and add the <code>platform-tools</code> folder to your system PATH.";
    }
  }

  const tools = [info.uv, info.adb, info.pyestro, info.pyestroWeb];
  document.getElementById("tool-status-list").innerHTML = tools
    .map((tool) => {
      const pillState = tool.found ? "ok" : "err";
      const pillText = tool.found ? "Found" : "Missing";
      const pathText = tool.found
        ? tool.pythonPath
          ? `${tool.path} (python: ${tool.pythonPath})`
          : tool.path
        : tool.hint || "Not found";
      return `
        <div class="boxed-row">
          <div class="row-meta">
            <div class="row-title-line">
              <span class="row-title">${escapeHTML(tool.name)}</span>
              ${tool.version ? `<span class="mono-dim">${escapeHTML(tool.version)}</span>` : ""}
            </div>
            <div class="row-subtitle row-mono">${escapeHTML(pathText)}</div>
          </div>
          <div class="row-suffix">
            <span class="status-pill muted">${escapeHTML(tool.source || "Unknown")}</span>
            <span class="status-pill ${pillState}">${pillText}</span>
          </div>
        </div>
      `;
    })
    .join("");

  const serialSelect = document.getElementById("android-serial");
  const prevVal = serialSelect.value;
  serialSelect.innerHTML = '<option value="">Default ADB device</option>';
  (info.devices || []).forEach((dev) => {
    const opt = document.createElement("option");
    opt.value = dev.serial;
    opt.textContent = `${dev.serial} (${dev.state})`;
    serialSelect.appendChild(opt);
  });
  if (prevVal) serialSelect.value = prevVal;

  const devContainer = document.getElementById("adb-devices-list");
  if (info.devices && info.devices.length > 0) {
    devContainer.innerHTML = info.devices
      .map(
        (d) =>
          `<div class="boxed-row"><div class="row-meta"><span class="row-title row-mono">${escapeHTML(d.serial)}</span></div><div class="row-suffix"><span class="status-pill ${d.state === "device" ? "ok" : "warn"}">${escapeHTML(d.state)}</span></div></div>`,
      )
      .join("");
  } else {
    devContainer.innerHTML =
      '<div class="boxed-row-empty">No ADB devices detected.</div>';
  }

  updateStudioEnvBanner();
}

async function refreshSystemInfo() {
  try {
    const info = await window.api_system_info();
    renderSystemInfo(info);
  } catch (err) {
    showToast("Failed to scan tools: " + err);
  }
}

async function installTool(target, btn) {
  const outEl = document.getElementById("uv-install-output");
  outEl.classList.remove("hidden");
  outEl.textContent =
    target === "uv"
      ? `Installing uv for ${formatOSName(systemInfo && systemInfo.os)}...\n`
      : `Installing ${target} via uv...\n`;

  const prevLabel = btn ? btn.textContent : "";
  if (btn) {
    btn.disabled = true;
    btn.textContent = "Installing…";
  }
  try {
    const res = await window.api_install_tool({ target });
    outEl.textContent = `${res.command ? "$ " + res.command + "\n\n" : ""}${res.output || ""}${res.error ? "\nError: " + res.error : ""}`;
    await refreshSystemInfo();
    showToast(
      res.ok ? `${target} installed/updated` : `Failed to install ${target}`,
    );
  } catch (err) {
    outEl.textContent += `\nError: ${err}`;
    showToast(`Failed to install ${target}: ` + err);
  } finally {
    if (btn) {
      btn.disabled = false;
      if (btn.textContent === "Installing…") {
        btn.textContent = prevLabel;
      }
    }
    await refreshSystemInfo();
  }
}

function updateSessionControls(connected) {
  sessionConnected = connected;
  document.getElementById("btn-refresh-snap").disabled = !connected;
  document.getElementById("btn-disconnect").disabled = !connected;
  document.getElementById("btn-run-step").disabled = !connected;
  document.getElementById("btn-quick-back").disabled = !connected;
  document.getElementById("btn-quick-kbd").disabled = !connected;
  setPill(
    "header-session-pill",
    connected ? "ok" : "muted",
    connected ? currentTarget : "Idle",
  );
}

function normalizeWebUrl(raw) {
  const value = (raw || "").trim();
  if (!value) return "";
  if (/^[a-zA-Z][a-zA-Z\d+\-.]*:\/\//.test(value)) return value;
  const isLocal =
    /^(localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\])(?::\d+)?(\/|$)/i.test(value);
  return (isLocal ? "http://" : "https://") + value;
}

async function connectSession() {
  const btn = document.getElementById("btn-connect");
  btn.disabled = true;
  btn.textContent = "Connecting...";
  try {
    const urlInput = document.getElementById("web-url");
    const normalizedUrl = normalizeWebUrl(urlInput.value);
    if (normalizedUrl) urlInput.value = normalizedUrl;
    const cfg = {
      target: currentTarget,
      serial: document.getElementById("android-serial").value,
      appId: document.getElementById("android-appid").value.trim(),
      url: normalizedUrl,
      browser: document.getElementById("web-browser").value,
      viewport: document.getElementById("web-viewport").value,
      headless: document.getElementById("web-headless").checked,
    };
    appendLog(`Starting ${currentTarget} studio session...`);
    const snap = await window.api_start_session(cfg);
    updateSessionControls(true);
    applySnapshot(snap);
    appendLog(`Connected (${currentTarget})`);
    showToast(`Connected (${currentTarget})`);
  } catch (err) {
    appendLog(`Session error: ${err}`);
    showToast("Connection failed: " + err);
  } finally {
    btn.disabled = false;
    btn.textContent = "Connect";
  }
}

async function disconnectSession() {
  try {
    await window.api_stop_session();
    updateSessionControls(false);
    document.getElementById("screen-meta").textContent = "Disconnected";
    showToast("Session disconnected");
  } catch (err) {
    showToast("Disconnect error: " + err);
  }
}

async function refreshSnapshot() {
  try {
    const snap = await window.api_refresh_snapshot();
    applySnapshot(snap);
  } catch (err) {
    appendLog(`Snapshot error: ${err}`);
    showToast("Snapshot failed: " + err);
  }
}

function fitScreenFrame() {
  const stage = document.getElementById("screen-stage");
  const frame = document.getElementById("screen-frame");
  const img = document.getElementById("screen-img");
  if (!stage || !frame || !img || frame.classList.contains("hidden")) return;

  const natW =
    img.naturalWidth || (currentSnapshot && currentSnapshot.width) || 0;
  const natH =
    img.naturalHeight || (currentSnapshot && currentSnapshot.height) || 0;
  if (!natW || !natH) return;

  const availW = Math.max(80, stage.clientWidth - 28);
  const availH = Math.max(80, stage.clientHeight - 28);
  const scale = Math.min(availW / natW, availH / natH);
  frame.style.width = Math.max(1, Math.floor(natW * scale)) + "px";
  frame.style.height = Math.max(1, Math.floor(natH * scale)) + "px";
}

function applySnapshot(snap) {
  if (!snap || !snap.imageData) return;
  currentSnapshot = snap;
  selectedElement = null;
  document.getElementById("empty-stage").classList.add("hidden");
  document.getElementById("screen-frame").classList.remove("hidden");
  const img = document.getElementById("screen-img");
  img.onload = () => {
    fitScreenFrame();
    renderOverlays();
  };
  img.src = snap.imageData;

  const metaParts = [
    `${snap.width}x${snap.height}`,
    `${(snap.elements || []).length} elements`,
  ];
  if (snap.currentUrl) metaParts.push(snap.currentUrl);
  if (snap.capturedAt) metaParts.push(snap.capturedAt);
  document.getElementById("screen-meta").textContent = metaParts.join(" · ");

  document.getElementById("tree-count").textContent = String(
    (snap.elements || []).length,
  );
  renderElementTree();
  renderInspector(null);
}

function renderOverlays() {
  const layer = document.getElementById("overlay-layer");
  layer.innerHTML = "";
  if (!currentSnapshot || !document.getElementById("toggle-overlays").checked) {
    return;
  }
  const w = currentSnapshot.width || 1;
  const h = currentSnapshot.height || 1;
  const elements = currentSnapshot.elements || [];

  const overlayElements = elements
    .filter((el) => {
      const [x1, y1, x2, y2] = el.bounds || [0, 0, 0, 0];
      if (x2 <= x1 || y2 <= y1) return false;
      const hasSemanticInfo = Boolean(
        el.clickable ||
        (el.text && el.text.trim()) ||
        el.resourceId ||
        el.domId ||
        el.contentDesc ||
        el.hint,
      );
      if (!hasSemanticInfo) return false;
      const bw = x2 - x1;
      const bh = y2 - y1;
      const isFullScreenWrapper =
        bw >= w * 0.92 && bh >= h * 0.92 && !el.clickable;
      return !isFullScreenWrapper;
    })
    .sort((a, b) => {
      const [ax1, ay1, ax2, ay2] = a.bounds;
      const [bx1, by1, bx2, by2] = b.bounds;
      return (bx2 - bx1) * (by2 - by1) - (ax2 - ax1) * (ay2 - ay1);
    });

  overlayElements.forEach((el) => {
    const [x1, y1, x2, y2] = el.bounds;
    const box = document.createElement("div");
    box.className = "overlay-box" + (el.clickable ? " clickable" : "");
    if (selectedElement && selectedElement.index === el.index) {
      box.classList.add("selected");
    }
    box.style.left = (x1 / w) * 100 + "%";
    box.style.top = (y1 / h) * 100 + "%";
    box.style.width = ((x2 - x1) / w) * 100 + "%";
    box.style.height = ((y2 - y1) / h) * 100 + "%";
    box.title =
      el.text ||
      el.resourceId ||
      el.contentDesc ||
      el.className ||
      `#${el.index}`;
    box.onclick = (e) => {
      e.stopPropagation();
      selectElement(el);
    };
    layer.appendChild(box);
  });
}

function selectElement(el) {
  selectedElement = el;
  renderOverlays();
  renderInspector(el);
  renderElementTree();
}

function renderInspector(el) {
  const empty = document.getElementById("inspector-empty");
  const details = document.getElementById("inspector-details");
  if (!el) {
    empty.classList.remove("hidden");
    details.classList.add("hidden");
    return;
  }
  empty.classList.add("hidden");
  details.classList.remove("hidden");

  document.getElementById("insp-title").textContent =
    el.text || el.resourceId || el.className || `Element #${el.index}`;
  document.getElementById("insp-bounds").textContent =
    `[${(el.bounds || []).join(", ")}]`;

  const attrs = [];
  if (el.resourceId)
    attrs.push(
      `<span><strong>id:</strong> ${escapeHTML(el.resourceId)}</span>`,
    );
  if (el.domId)
    attrs.push(`<span><strong>domId:</strong> ${escapeHTML(el.domId)}</span>`);
  if (el.text)
    attrs.push(`<span><strong>text:</strong> ${escapeHTML(el.text)}</span>`);
  if (el.contentDesc)
    attrs.push(
      `<span><strong>desc:</strong> ${escapeHTML(el.contentDesc)}</span>`,
    );
  if (el.hint)
    attrs.push(`<span><strong>hint:</strong> ${escapeHTML(el.hint)}</span>`);
  if (el.className)
    attrs.push(
      `<span><strong>class:</strong> ${escapeHTML(el.className)}</span>`,
    );
  attrs.push(
    `<span><strong>clickable:</strong> ${el.clickable ? "true" : "false"}</span>`,
  );
  document.getElementById("insp-attrs").innerHTML = attrs.join("");

  const chips = document.getElementById("insp-selectors");
  chips.innerHTML = "";
  (el.selectors || []).forEach((sel) => {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "selector-chip";
    btn.textContent = sel;
    btn.onclick = async () => {
      try {
        await window.api_copy_text(sel);
        showToast("Copied selector: " + sel);
      } catch (_) {}
    };
    chips.appendChild(btn);
  });
}

function preferredSelectorYAML(el) {
  if (!el || !el.selectors || el.selectors.length === 0) return null;
  return el.selectors[0];
}

function buildCommandSnippet(cmdName, el) {
  const sel = preferredSelectorYAML(el);
  if (!sel) return `- ${cmdName}`;
  if (sel.startsWith("text: ")) {
    return `- ${cmdName}: ${sel.slice("text: ".length)}`;
  }
  return `- ${cmdName}:\n    ${sel}`;
}

function appendToYamlEditor(snippet) {
  const editor = document.getElementById("yaml-editor");
  const prefix = editor.value.endsWith("\n") || editor.value === "" ? "" : "\n";
  editor.value += prefix + snippet + "\n";
  editor.scrollTop = editor.scrollHeight;
  updateFlowStateUI();
}

function insertSelectedCommand(cmdName) {
  if (!selectedElement) return;
  appendToYamlEditor(buildCommandSnippet(cmdName, selectedElement));
  showToast(`Inserted ${cmdName}`);
}

async function runSelectedTap() {
  if (!selectedElement) return;
  await quickStep(buildCommandSnippet("tapOn", selectedElement));
}

async function runSelectedType() {
  if (!selectedElement) return;
  const text = document.getElementById("insp-type-text").value;
  if (!text) {
    showToast("Enter text to type");
    return;
  }
  const tapCmd = buildCommandSnippet("tapOn", selectedElement);
  const quoted = JSON.stringify(text);
  await quickStep(`${tapCmd}\n- inputText: ${quoted}`);
}

function renderElementTree() {
  const container = document.getElementById("element-tree-list");
  if (!currentSnapshot || !currentSnapshot.elements) {
    container.innerHTML = "";
    return;
  }
  const query = (document.getElementById("tree-filter").value || "")
    .trim()
    .toLowerCase();
  const filtered = currentSnapshot.elements.filter((el) => {
    if (!query) return true;
    return [el.text, el.resourceId, el.domId, el.contentDesc, el.className]
      .join(" ")
      .toLowerCase()
      .includes(query);
  });

  container.innerHTML = "";
  filtered.slice(0, 250).forEach((el) => {
    const row = document.createElement("div");
    row.className =
      "tree-row" +
      (selectedElement && selectedElement.index === el.index
        ? " selected"
        : "");
    const label =
      el.text ||
      el.resourceId ||
      el.contentDesc ||
      el.className ||
      `#${el.index}`;
    row.innerHTML = `
      <span class="tree-row-main"><strong>${escapeHTML(label)}</strong> <span class="mono-dim">${escapeHTML(el.className || "")}</span></span>
      <span class="mono-dim">[${(el.bounds || []).join(",")}]</span>
    `;
    row.onclick = () => selectElement(el);
    container.appendChild(row);
  });
}

async function quickStep(yamlSnippet) {
  if (!sessionConnected) {
    showToast("Connect to a device or browser first");
    return;
  }
  appendLog(`> ${yamlSnippet.replace(/\n/g, " ; ")}`);
  try {
    const flowYaml = (document.getElementById("yaml-editor") || {}).value || "";
    const res = await window.api_run_step({
      yaml: yamlSnippet,
      flowPath: currentFlowPath,
      flowYaml,
      env: currentEnvConfig.env,
      envFile: currentEnvConfig.envFile,
    });
    if (res.snapshot) applySnapshot(res.snapshot);
    if (res.ok) {
      appendLog(`PASS (${res.durationMs}ms)`);
    } else {
      appendLog(`FAIL: ${res.error}`);
      showToast("Step failed: " + res.error);
    }
  } catch (err) {
    appendLog(`Step error: ${err}`);
    showToast("Step error: " + err);
  }
}

async function runReplStep() {
  const input = document.getElementById("repl-input");
  let snippet = input.value.trim();
  if (!snippet) return;
  if (!snippet.startsWith("-")) {
    snippet = "- " + snippet;
  }
  await quickStep(snippet);
}

async function validateCurrentFlow() {
  const yaml = document.getElementById("yaml-editor").value;
  try {
    const res = await window.api_validate_yaml({
      target: currentTarget,
      yaml,
      flowPath: currentFlowPath,
      env: currentEnvConfig.env,
      envFile: currentEnvConfig.envFile,
    });
    if (res.valid) {
      setPill("yaml-valid-pill", "ok", `Valid (${res.steps} steps)`);
      showToast(`Valid Maestro YAML (${res.steps} steps)`);
    } else {
      setPill("yaml-valid-pill", "err", "Invalid");
      appendLog(`Validation error: ${res.error}`);
      showToast("Invalid YAML: " + res.error);
    }
  } catch (err) {
    setPill("yaml-valid-pill", "err", "Error");
    showToast("Validation failed: " + err);
  }
}

async function runCurrentFlow() {
  const btn = document.getElementById("btn-run-flow");
  if (btn.disabled) return;
  const labelEl = btn.querySelector("span");
  btn.disabled = true;
  if (labelEl) {
    labelEl.textContent = "Running...";
  } else {
    btn.textContent = "Running...";
  }
  const yaml = document.getElementById("yaml-editor").value;
  try {
    const urlInput = document.getElementById("web-url");
    const normalizedUrl = normalizeWebUrl(urlInput.value);
    if (normalizedUrl) urlInput.value = normalizedUrl;
    const res = await window.api_run_flow({
      target: currentTarget,
      yaml,
      flowPath: currentFlowPath,
      serial: document.getElementById("android-serial").value,
      url: normalizedUrl,
      browser: document.getElementById("web-browser").value,
      viewport: document.getElementById("web-viewport").value,
      headless: document.getElementById("web-headless").checked,
      record: document.getElementById("opt-record").checked,
      report: document.getElementById("opt-report").checked,
      env: currentEnvConfig.env,
      envFile: currentEnvConfig.envFile,
    });
    const summary = `Flow finished: ${res.passed}/${res.total} passed (${res.durationMs}ms)`;
    appendLog(summary);
    if (res.videoPath) {
      appendLog(`Video saved: ${res.videoPath}`);
    }
    if (res.reportPath) {
      appendLog(`Report saved: ${res.reportPath}`);
    }
    showToast(res.ok ? summary : `Flow failed (${res.failed} failed)`);
  } catch (err) {
    appendLog(`Flow run error: ${err}`);
    showToast("Run failed: " + err);
  } finally {
    btn.disabled = false;
    if (labelEl) {
      labelEl.textContent = "Run Flow";
    } else {
      btn.textContent = "Run Flow";
    }
  }
}

function isFlowDirty() {
  const editor = document.getElementById("yaml-editor");
  return Boolean(editor && editor.value !== lastSavedContent);
}

function fileBaseName(path) {
  if (!path) return "Unsaved flow";
  const parts = String(path).split(/[\\/]/);
  return parts[parts.length - 1] || path;
}

function updateFlowStateUI() {
  renderYamlHighlight();
  const dirty = isFlowDirty();
  const dirtyPill = document.getElementById("yaml-dirty-pill");
  if (dirtyPill) {
    dirtyPill.classList.toggle("hidden", !dirty);
  }
  const pathEl = document.getElementById("header-flow-path");
  if (pathEl) {
    const label = currentFlowPath ? currentFlowPath : "Unsaved flow";
    pathEl.textContent = dirty ? `• ${label}` : label;
  }
  const saveBtn = document.getElementById("btn-save-flow");
  if (saveBtn) {
    const canSkipSave = Boolean(currentFlowPath) && !dirty;
    saveBtn.disabled = canSkipSave;
    saveBtn.title = canSkipSave ? "No unsaved changes" : "Save Flow (Cmd+S)";
  }
}

function showAdwDialog({ title, body, path, buttons }) {
  return new Promise((resolve) => {
    const backdrop = document.getElementById("adw-dialog-backdrop");
    const titleEl = document.getElementById("adw-dialog-title");
    const bodyEl = document.getElementById("adw-dialog-body");
    const pathEl = document.getElementById("adw-dialog-path");
    const actionsEl = document.getElementById("adw-dialog-actions");

    titleEl.textContent = title;
    bodyEl.textContent = body;
    if (path) {
      pathEl.textContent = path;
      pathEl.classList.remove("hidden");
    } else {
      pathEl.classList.add("hidden");
    }

    actionsEl.innerHTML = "";
    const cleanup = (result) => {
      backdrop.classList.add("hidden");
      window.removeEventListener("keydown", onKeyDown);
      resolve(result);
    };
    const onKeyDown = (e) => {
      if (e.key === "Escape") {
        e.preventDefault();
        cleanup(null);
      }
    };
    window.addEventListener("keydown", onKeyDown);

    buttons.forEach((btnDef) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "adw-btn " + (btnDef.variant || "");
      btn.textContent = btnDef.label;
      btn.onclick = () => cleanup(btnDef.id);
      actionsEl.appendChild(btn);
    });

    backdrop.classList.remove("hidden");
  });
}

async function confirmDiscardIfDirty() {
  if (!isFlowDirty()) return true;
  const choice = await showAdwDialog({
    title: "Discard unsaved changes?",
    body: `Your changes to "${fileBaseName(currentFlowPath)}" have not been saved.`,
    path: currentFlowPath || "",
    buttons: [
      { id: "save", label: "Save Changes…", variant: "adw-btn-primary" },
      {
        id: "discard",
        label: "Discard Changes",
        variant: "adw-btn-destructive",
      },
      { id: "cancel", label: "Cancel" },
    ],
  });
  if (choice === "discard") return true;
  if (choice === "save") {
    return await saveFlow(false);
  }
  return false;
}

async function newFlowTemplate() {
  if (!(await confirmDiscardIfDirty())) return;
  const nextTemplate =
    currentTarget === "web" ? DEFAULT_WEB_YAML : DEFAULT_ANDROID_YAML;
  currentFlowPath = "";
  currentFlowHash = "";
  document.getElementById("yaml-editor").value = nextTemplate;
  lastSavedContent = nextTemplate;
  updateFlowStateUI();
  setPill("yaml-valid-pill", "muted", "Unchecked");
  await loadEnvConfigForCurrentFlow();
}

async function openFlowDialog() {
  if (!(await confirmDiscardIfDirty())) return;
  try {
    const res = await window.api_open_flow_file();
    if (res && res.path) {
      currentFlowPath = res.path;
      currentFlowHash = res.hash || "";
      const content = res.content || "";
      document.getElementById("yaml-editor").value = content;
      lastSavedContent = content;
      updateFlowStateUI();
      setPill("yaml-valid-pill", "muted", "Unchecked");
      await loadEnvConfigForCurrentFlow();
      await validateCurrentFlow();
    }
  } catch (err) {
    showToast("Failed to open flow: " + err);
  }
}

async function saveFlow(saveAs) {
  const content = document.getElementById("yaml-editor").value;
  if (!saveAs && currentFlowPath && !isFlowDirty()) {
    showToast("No unsaved changes");
    return true;
  }

  let force = false;
  if (!saveAs && currentFlowPath) {
    const choice = await showAdwDialog({
      title: `Overwrite "${fileBaseName(currentFlowPath)}"?`,
      body: "Saving will replace the existing file on disk. Use Save As to keep the original file.",
      path: currentFlowPath,
      buttons: [
        { id: "save-as", label: "Save As…", variant: "adw-btn-primary" },
        {
          id: "overwrite",
          label: "Overwrite File",
          variant: "adw-btn-destructive",
        },
        { id: "cancel", label: "Cancel" },
      ],
    });
    if (choice === "save-as") {
      saveAs = true;
    } else if (choice !== "overwrite") {
      return false;
    }
  }

  try {
    let res = saveAs
      ? await window.api_save_flow_file_as({ path: currentFlowPath, content })
      : await window.api_save_flow_file({
          path: currentFlowPath,
          content,
          expectedHash: currentFlowHash,
          force,
        });
    if (res && res.path) {
      currentFlowPath = res.path;
      currentFlowHash = res.hash || "";
      lastSavedContent = content;
      updateFlowStateUI();
      showToast("Flow saved");
      return true;
    }
    return false;
  } catch (err) {
    const msg = String(err || "");
    if (!saveAs && msg.includes("changed on disk")) {
      const conflictChoice = await showAdwDialog({
        title: "File changed on disk",
        body: `"${fileBaseName(currentFlowPath)}" was modified by another program after you opened it.`,
        path: currentFlowPath,
        buttons: [
          {
            id: "save-as",
            label: "Save As New File…",
            variant: "adw-btn-primary",
          },
          {
            id: "force",
            label: "Overwrite Anyway",
            variant: "adw-btn-destructive",
          },
          { id: "cancel", label: "Cancel" },
        ],
      });
      if (conflictChoice === "save-as") {
        return await saveFlow(true);
      }
      if (conflictChoice === "force") {
        const res = await window.api_save_flow_file({
          path: currentFlowPath,
          content,
          force: true,
        });
        if (res && res.path) {
          currentFlowPath = res.path;
          currentFlowHash = res.hash || "";
          lastSavedContent = content;
          updateFlowStateUI();
          showToast("Flow saved");
          return true;
        }
      }
      return false;
    }
    showToast("Failed to save flow: " + err);
    return false;
  }
}

async function openExternalLink(url) {
  try {
    await window.api_open_external(url);
  } catch (_) {
    window.open(url, "_blank");
  }
}

if (window.glaze && glaze.events) {
  glaze.events.on("studio:log", (msg) => {
    if (msg) appendLog(msg);
  });
  glaze.events.on("studio:snapshot", (snap) => {
    if (snap) applySnapshot(snap);
  });
  glaze.events.on("studio:status", (st) => {
    if (st) updateSessionControls(!!st.connected);
  });
  glaze.events.on("studio:system-updated", (info) => {
    if (info) renderSystemInfo(info);
  });
  glaze.events.on("app:navigate", (page) => {
    if (page) switchPage(page);
  });
  glaze.events.on("app:action", (action) => {
    if (action === "open-flow") openFlowDialog();
    if (action === "save-flow") saveFlow(false);
    if (action === "save-flow-as") saveFlow(true);
    if (action === "refresh-snapshot" && sessionConnected) refreshSnapshot();
    if (action === "validate-flow") validateCurrentFlow();
    if (action === "run-flow") runCurrentFlow();
  });
}

window.addEventListener("DOMContentLoaded", async () => {
  const editor = document.getElementById("yaml-editor");
  editor.value = DEFAULT_ANDROID_YAML;
  lastSavedContent = DEFAULT_ANDROID_YAML;
  updateFlowStateUI();

  editor.addEventListener("input", updateFlowStateUI);
  editor.addEventListener("scroll", syncYamlScroll);

  document.getElementById("repl-input").addEventListener("keydown", (e) => {
    if (e.key === "Enter") {
      e.preventDefault();
      runReplStep();
    }
  });

  editor.addEventListener("keydown", (e) => {
    if (e.key === "Tab") {
      e.preventDefault();
      const el = e.target;
      const start = el.selectionStart;
      const end = el.selectionEnd;
      el.value = el.value.slice(0, start) + "  " + el.value.slice(end);
      el.selectionStart = el.selectionEnd = start + 2;
      updateFlowStateUI();
    }
  });

  const stageEl = document.getElementById("screen-stage");
  if (stageEl && typeof ResizeObserver !== "undefined") {
    new ResizeObserver(() => fitScreenFrame()).observe(stageEl);
  }
  window.addEventListener("resize", fitScreenFrame);
  window.addEventListener("keydown", (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      runCurrentFlow();
    }
  });

  await refreshSystemInfo();
  await loadEnvConfigForCurrentFlow();
  initTooltips();
});

function escapeHtml(str) {
  return String(str)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function syncYamlScroll() {
  const editor = document.getElementById("yaml-editor");
  const highlight = document.getElementById("yaml-highlight");
  if (!editor || !highlight) return;
  highlight.scrollTop = editor.scrollTop;
  highlight.scrollLeft = editor.scrollLeft;
}

function highlightYamlVars(escaped) {
  return escaped.replace(
    /\$\{[A-Za-z0-9_]+\}/g,
    '<span class="hl-var">$&</span>',
  );
}

function highlightYamlValue(rawVal) {
  if (!rawVal) return "";
  const trimmed = rawVal.trim();
  const lead = rawVal.slice(0, rawVal.indexOf(trimmed));
  const trail = rawVal.slice(rawVal.indexOf(trimmed) + trimmed.length);

  if (
    (trimmed.startsWith('"') && trimmed.endsWith('"') && trimmed.length >= 2) ||
    (trimmed.startsWith("'") && trimmed.endsWith("'") && trimmed.length >= 2)
  ) {
    return `${escapeHtml(lead)}<span class="hl-string">${highlightYamlVars(escapeHtml(trimmed))}</span>${escapeHtml(trail)}`;
  }
  if (/^(true|false|null|~|-?\d+(?:\.\d+)?)$/i.test(trimmed)) {
    return `${escapeHtml(lead)}<span class="hl-number">${escapeHtml(trimmed)}</span>${escapeHtml(trail)}`;
  }
  return highlightYamlVars(escapeHtml(rawVal));
}

function highlightYamlLine(line) {
  if (/^\s*---+\s*$/.test(line)) {
    return `<span class="hl-punct">${escapeHtml(line)}</span>`;
  }
  if (/^\s*#/.test(line)) {
    return `<span class="hl-comment">${escapeHtml(line)}</span>`;
  }

  let body = line;
  let commentPart = "";
  let inSingle = false;
  let inDouble = false;
  for (let i = 0; i < line.length; i += 1) {
    const ch = line[i];
    if (ch === "'" && !inDouble) inSingle = !inSingle;
    else if (ch === '"' && !inSingle && line[i - 1] !== "\\")
      inDouble = !inDouble;
    else if (
      ch === "#" &&
      !inSingle &&
      !inDouble &&
      (i === 0 || /\s/.test(line[i - 1]))
    ) {
      body = line.slice(0, i);
      commentPart = `<span class="hl-comment">${escapeHtml(line.slice(i))}</span>`;
      break;
    }
  }

  const dashMatch = body.match(/^(\s*-\s+)(.*)$/);
  let prefixHtml = "";
  let rest = body;
  let isListItem = false;
  if (dashMatch) {
    prefixHtml = `${escapeHtml(dashMatch[1].slice(0, -dashMatch[1].trimStart().length))}<span class="hl-punct">${escapeHtml(dashMatch[1].trimStart())}</span>`;
    rest = dashMatch[2];
    isListItem = true;
  }

  const kvMatch = rest.match(/^(\s*)([A-Za-z0-9_.-]+)(\s*:)(.*)$/);
  if (kvMatch) {
    const [, indent, key, colon, val] = kvMatch;
    return `${prefixHtml}${escapeHtml(indent)}<span class="hl-key">${escapeHtml(key)}</span><span class="hl-punct">${escapeHtml(colon)}</span>${highlightYamlValue(val)}${commentPart}`;
  }

  if (isListItem && /^[A-Za-z0-9_.-]+\s*$/.test(rest)) {
    return `${prefixHtml}<span class="hl-cmd">${escapeHtml(rest)}</span>${commentPart}`;
  }

  return `${prefixHtml}${highlightYamlValue(rest)}${commentPart}`;
}

function renderYamlHighlight() {
  const editor = document.getElementById("yaml-editor");
  const highlight = document.getElementById("yaml-highlight");
  if (!editor || !highlight) return;
  const text = editor.value || "";
  const html = text.split("\n").map(highlightYamlLine).join("\n");
  highlight.innerHTML = text.endsWith("\n") ? `${html} ` : html;
  syncYamlScroll();
}

function initTooltips() {
  const tip = document.createElement("div");
  tip.className = "adw-tooltip";
  tip.setAttribute("role", "tooltip");
  tip.setAttribute("aria-hidden", "true");
  document.body.appendChild(tip);

  let activeEl = null;
  let showTimer = null;

  function readTooltipText(el) {
    if (!el) return "";
    if (el.hasAttribute("title")) {
      const t = el.getAttribute("title") || "";
      el.removeAttribute("title");
      if (t) {
        el.setAttribute("data-tooltip", t);
      } else {
        el.removeAttribute("data-tooltip");
      }
      return t;
    }
    return el.getAttribute("data-tooltip") || "";
  }

  function positionTip(el) {
    const rect = el.getBoundingClientRect();
    const tipRect = tip.getBoundingClientRect();
    const margin = 8;
    const gap = 6;

    let left = rect.left + rect.width / 2 - tipRect.width / 2;
    left = Math.max(
      margin,
      Math.min(left, window.innerWidth - tipRect.width - margin),
    );

    let top = rect.bottom + gap;
    if (
      top + tipRect.height + margin > window.innerHeight &&
      rect.top - tipRect.height - gap >= margin
    ) {
      top = rect.top - tipRect.height - gap;
    }

    tip.style.left = `${Math.round(left)}px`;
    tip.style.top = `${Math.round(top)}px`;
  }

  function hideTip() {
    clearTimeout(showTimer);
    showTimer = null;
    activeEl = null;
    tip.classList.remove("visible");
    tip.setAttribute("aria-hidden", "true");
  }

  function scheduleShow(el, immediate) {
    const text = readTooltipText(el);
    if (!text) return;
    clearTimeout(showTimer);
    activeEl = el;
    const display = () => {
      if (activeEl !== el) return;
      const latest = readTooltipText(el);
      if (!latest) return;
      tip.textContent = latest;
      tip.setAttribute("aria-hidden", "false");
      positionTip(el);
      tip.classList.add("visible");
    };
    if (immediate) {
      display();
    } else {
      showTimer = setTimeout(display, 260);
    }
  }

  document.addEventListener("mouseover", (e) => {
    const target =
      e.target && e.target.closest
        ? e.target.closest("[title], [data-tooltip]")
        : null;
    if (!target) {
      if (activeEl) hideTip();
      return;
    }
    if (target === activeEl) return;
    scheduleShow(target, false);
  });

  document.addEventListener("mouseout", (e) => {
    if (!activeEl) return;
    const related =
      e.relatedTarget && e.relatedTarget.closest
        ? e.relatedTarget.closest("[title], [data-tooltip]")
        : null;
    if (related !== activeEl) {
      hideTip();
    }
  });

  document.addEventListener("focusin", (e) => {
    const target =
      e.target && e.target.closest
        ? e.target.closest("[title], [data-tooltip]")
        : null;
    if (target) scheduleShow(target, true);
  });

  document.addEventListener("focusout", hideTip);
  document.addEventListener("mousedown", hideTip);
  document.addEventListener("keydown", hideTip);
  window.addEventListener("scroll", hideTip, true);
}
