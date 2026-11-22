"""Persistent JSON-RPC sidecar bridge between Pyestro Studio (Go/Glaze) and pyestro / pyestro-web."""

from __future__ import annotations

import base64
import importlib
import inspect
import json
import os
import re
import sys
import tempfile
import time
import traceback
from contextlib import suppress
from pathlib import Path
from typing import Any

import yaml

TARGET = os.environ.get("PYESTRO_STUDIO_TARGET", "android").strip().lower()


def _emit_log(message: str) -> None:
    payload = {"event": "log", "message": str(message)}
    sys.stdout.write(json.dumps(payload, ensure_ascii=False) + "\n")
    sys.stdout.flush()


def _emit_snapshot(snap: dict[str, Any]) -> None:
    payload = {"event": "snapshot", "snapshot": snap}
    sys.stdout.write(json.dumps(payload, ensure_ascii=False) + "\n")
    sys.stdout.flush()


def _yaml_quote(value: str) -> str:
    escaped = value.replace("\\", "\\\\").replace('"', '\\"').replace("\n", "\\n")
    return f'"{escaped}"'


def _build_selector_suggestions(el: Any, target: str) -> list[str]:
    suggestions: list[str] = []
    res_id = getattr(el, "resource_id", "") or ""
    dom_id = getattr(el, "dom_id", "") or ""
    text = (getattr(el, "text", "") or "").strip()
    content_desc = (getattr(el, "content_desc", "") or "").strip()
    hint = (getattr(el, "hint", "") or "").strip()

    if res_id:
        short_id = res_id.split(":id/")[-1]
        suggestions.append(f"id: {_yaml_quote(short_id)}")
    elif dom_id and target == "web":
        suggestions.append(f"id: {_yaml_quote(dom_id)}")

    if text and len(text) <= 80:
        suggestions.append(f"text: {_yaml_quote(text)}")
    if content_desc and content_desc != text and len(content_desc) <= 80:
        suggestions.append(f"text: {_yaml_quote(content_desc)}")
    if hint and hint not in (text, content_desc) and len(hint) <= 80:
        suggestions.append(f"text: {_yaml_quote(hint)}")

    x1, y1, x2, y2 = el.bounds
    cx, cy = (x1 + x2) // 2, (y1 + y2) // 2
    suggestions.append(f'point: "({cx},{cy})"')
    return suggestions


def _serialize_elements(elements: list[Any], target: str) -> list[dict[str, Any]]:
    serialized: list[dict[str, Any]] = []
    for el in elements:
        x1, y1, x2, y2 = el.bounds
        if not getattr(el, "visible", True) or x2 <= x1 or y2 <= y1:
            continue
        serialized.append(
            {
                "index": int(el.index),
                "parent": int(el.parent) if el.parent is not None else None,
                "text": str(getattr(el, "text", "") or ""),
                "hint": str(getattr(el, "hint", "") or ""),
                "resourceId": str(getattr(el, "resource_id", "") or ""),
                "contentDesc": str(getattr(el, "content_desc", "") or ""),
                "className": str(getattr(el, "class_name", "") or ""),
                "domId": str(getattr(el, "dom_id", "") or ""),
                "bounds": [int(x1), int(y1), int(x2), int(y2)],
                "clickable": bool(getattr(el, "clickable", False)),
                "enabled": bool(getattr(el, "enabled", True)),
                "checked": bool(getattr(el, "checked", False)),
                "focused": bool(getattr(el, "focused", False)),
                "selected": bool(getattr(el, "selected", False)),
                "visible": True,
                "selectors": _build_selector_suggestions(el, target),
            }
        )
    return serialized


def _normalize_url(raw: str) -> str:
    value = (raw or "").strip()
    if not value:
        return ""
    if re.match(r"^[a-zA-Z][a-zA-Z\d+\-.]*://", value):
        return value
    is_local = bool(
        re.match(
            r"^(localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\])(?::\d+)?(/|$)",
            value,
            re.IGNORECASE,
        )
    )
    return ("http://" if is_local else "https://") + value


def _parse_viewport(value: str) -> tuple[int, int]:
    presets = {
        "desktop": (1280, 720),
        "mobile": (390, 844),
        "tablet": (768, 1024),
    }
    cleaned = (value or "mobile").strip().lower()
    if cleaned in presets:
        return presets[cleaned]
    m = re.fullmatch(r"(\d+)\s*[x,]\s*(\d+)", cleaned)
    if m:
        return max(100, int(m.group(1))), max(100, int(m.group(2)))
    return (390, 844) if TARGET == "web" else (1080, 1920)


def _coerce_env_dict(raw: Any) -> dict[str, str]:
    if not isinstance(raw, dict):
        return {}
    return {
        str(k).strip(): "" if v is None else str(v)
        for k, v in raw.items()
        if str(k).strip()
    }


def _default_artifacts_dir() -> Path:
    cwd = Path.cwd().resolve()
    if cwd != cwd.parent and os.access(cwd, os.W_OK):
        return cwd / "artifacts"
    home = Path.home()
    candidates = []
    docs = home / "Documents"
    if docs.is_dir():
        candidates.append(docs / "Pyestro Studio" / "artifacts")
    candidates.append(home / "Pyestro Studio" / "artifacts")
    for candidate in candidates:
        try:
            candidate.mkdir(parents=True, exist_ok=True)
            if os.access(candidate, os.W_OK):
                return candidate
        except OSError:
            continue
    return Path(tempfile.gettempdir()) / "pyestro-studio" / "artifacts"


def _fallback_find_env_file(start: Path | None) -> Path | None:
    base = start.resolve() if start is not None else Path.cwd().resolve()
    if base.is_file() or (not base.exists() and base.suffix in (".yaml", ".yml")):
        base = base.parent
    home = Path.home().resolve()
    for directory in (base, *base.parents):
        if directory == home and base != home:
            break
        candidate = directory / ".env"
        if candidate.is_file():
            return candidate
        if any(
            (directory / marker).exists()
            for marker in (".git", "pyproject.toml", "package.json")
        ):
            break
        if directory == directory.parent:
            break
    return None


class StudioSession:
    def __init__(self, target: str) -> None:
        self.target = target
        self.pkg_name = "pyestro_web" if target == "web" else "pyestro"
        self.pkg = importlib.import_module(self.pkg_name)
        self.flow_mod = importlib.import_module(f"{self.pkg_name}.flow")
        self.runner_mod = importlib.import_module(f"{self.pkg_name}.runner")
        self.selectors_mod = importlib.import_module(f"{self.pkg_name}.selectors")
        self.driver: Any = None
        self.runner: Any = None
        self.session_cfg: dict[str, Any] = {}

    def close(self) -> None:
        if self.driver is not None and hasattr(self.driver, "close"):
            with suppress(Exception):
                self.driver.close()
        self.driver = None
        self.runner = None

    def ping(self) -> dict[str, Any]:
        return {
            "target": self.target,
            "package": self.pkg_name,
            "python": sys.executable,
            "version": getattr(self.pkg, "__version__", "0.1.0"),
        }

    def start_session(self, params: dict[str, Any]) -> dict[str, Any]:
        self.close()
        self.session_cfg = dict(params)
        logs: list[str] = []

        def collect_echo(msg: str) -> None:
            logs.append(str(msg))
            _emit_log(msg)

        if self.target == "web":
            web_mod = importlib.import_module("pyestro_web.web")
            url = _normalize_url(params.get("url") or "http://localhost:8081")
            browser = (params.get("browser") or "chromium").strip().lower()
            viewport = _parse_viewport(params.get("viewport") or "mobile")
            headless = bool(params.get("headless", True))
            self.driver = web_mod.PlaywrightDriver(
                browser_name=browser,
                headless=headless,
                viewport=viewport,
                base_url=url,
            )
            self.driver.start()
            self.driver.launch_app(url)
            self.runner = self.runner_mod.FlowRunner(
                self.driver, base_url=url, echo=collect_echo
            )
            self.runner._flow_url = url
        else:
            android_mod = importlib.import_module("pyestro.android")
            serial = (params.get("serial") or "").strip() or None
            adb_path = (params.get("adbPath") or "adb").strip() or "adb"
            app_id = (params.get("appId") or "").strip() or None
            self.driver = android_mod.AndroidDriver(serial=serial, adb_path=adb_path)
            self.runner = self.runner_mod.FlowRunner(self.driver, echo=collect_echo)
            self.runner._app_id = app_id
            if app_id:
                self.driver.launch_app(app_id)

        snap = self.snapshot()
        return {"connected": True, "snapshot": snap, "logs": logs}

    def snapshot(self) -> dict[str, Any]:
        if self.driver is None:
            raise RuntimeError(
                "No active studio session. Connect to a device or browser first."
            )
        return self.snapshot_for(self.driver)

    def snapshot_for(self, driver: Any) -> dict[str, Any]:
        with tempfile.TemporaryDirectory(prefix="pyestro-studio-") as tmp:
            png_path = Path(tmp) / "screen.png"
            driver.screenshot(png_path)
            img_b64 = base64.b64encode(png_path.read_bytes()).decode("ascii")

        width, height = driver.screen_size()
        current_url = ""
        if self.target == "web":
            raw_elements = driver.dump_elements()
            current_url = driver.current_url()
        else:
            root = driver.dump_hierarchy()
            raw_elements = self.selectors_mod.parse_elements(root)

        elements = _serialize_elements(raw_elements, self.target)
        return {
            "target": self.target,
            "width": int(width),
            "height": int(height),
            "imageData": f"data:image/png;base64,{img_b64}",
            "currentUrl": current_url,
            "elements": elements,
            "capturedAt": time.strftime("%H:%M:%S"),
        }

    def _resolve_flow_path(self, params: dict[str, Any]) -> Path | None:
        raw = (params.get("flowPath") or "").strip()
        return Path(raw).resolve() if raw else None

    def _resolve_env_file(
        self, params: dict[str, Any], flow_path: Path | None
    ) -> Path | None:
        raw = (params.get("envFile") or "").strip()
        if not raw:
            return None
        path = Path(raw).expanduser()
        if not path.is_absolute() and flow_path is not None:
            path = flow_path.parent / path
        return path

    def _resolve_dotenv_vars(
        self, params: dict[str, Any], flow_path: Path | None
    ) -> dict[str, str]:
        env_file = self._resolve_env_file(params, flow_path)
        if hasattr(self.flow_mod, "resolve_dotenv"):
            return self.flow_mod.resolve_dotenv(flow_path, env_file)
        if env_file is not None:
            if not env_file.is_file():
                raise self.flow_mod.FlowError(f"Env file not found: {env_file}")
            return self.flow_mod.load_env_file(env_file)
        merged: dict[str, str] = {}
        flow_env = _fallback_find_env_file(flow_path) if flow_path else None
        if flow_env is not None:
            merged.update(self.flow_mod.load_env_file(flow_env))
        cwd_env = _fallback_find_env_file(Path.cwd())
        if cwd_env is not None and cwd_env != flow_env:
            merged.update(self.flow_mod.load_env_file(cwd_env))
        return merged

    def _make_runner(
        self,
        driver: Any,
        params: dict[str, Any],
        flow_path: Path | None,
        **kwargs: Any,
    ) -> Any:
        overrides = _coerce_env_dict(params.get("env"))
        env_file = self._resolve_env_file(params, flow_path)
        init_params = inspect.signature(self.runner_mod.FlowRunner.__init__).parameters
        if "env_file" in init_params:
            return self.runner_mod.FlowRunner(
                driver, env=overrides, env_file=env_file, **kwargs
            )
        dotenv_vars = self._resolve_dotenv_vars(params, flow_path)
        return self.runner_mod.FlowRunner(
            driver, env={**dotenv_vars, **overrides}, **kwargs
        )

    def _build_step_env(self, params: dict[str, Any]) -> dict[str, str]:
        flow_path = self._resolve_flow_path(params)
        dotenv_vars = self._resolve_dotenv_vars(params, flow_path)
        os_env = (
            self.runner_mod._env_from_os()
            if hasattr(self.runner_mod, "_env_from_os")
            else {}
        )
        header_env: dict[str, str] = {}
        flow_yaml = params.get("flowYaml") or ""
        if flow_yaml.strip():
            with suppress(Exception):
                docs = list(yaml.safe_load_all(flow_yaml))
                if len(docs) >= 2 and isinstance(docs[0], dict):
                    header_env = self.flow_mod.parse_config(docs[0]).env
        session_env = dict(getattr(self.runner, "_env", {}) or {})
        overrides = _coerce_env_dict(params.get("env"))
        return {**dotenv_vars, **os_env, **session_env, **header_env, **overrides}

    def run_step(self, params: dict[str, Any]) -> dict[str, Any]:
        if self.runner is None or self.driver is None:
            raise RuntimeError(
                "No active studio session. Connect to a device or browser first."
            )

        snippet = (params.get("yaml") or "").strip()
        if not snippet:
            raise ValueError("Enter a Maestro YAML command to execute.")

        flow_path = params.get("flowPath")
        if flow_path:
            self.runner._base_dir = Path(flow_path).resolve().parent

        parsed = yaml.safe_load(snippet)
        if isinstance(parsed, (dict, str)):
            raw_commands = [parsed]
        elif isinstance(parsed, list):
            raw_commands = parsed
        else:
            raise TypeError(
                "Command snippet must be a YAML command or list of commands."
            )

        logs: list[str] = []
        original_echo = self.runner.echo
        original_env = dict(getattr(self.runner, "_env", {}) or {})
        self.runner.echo = lambda msg: (logs.append(str(msg)), _emit_log(msg))

        started = time.monotonic()
        try:
            env = self._build_step_env(params)
            self.runner._env = env
            commands = self.flow_mod.compile_commands(raw_commands, env)
            self.runner._run_commands(commands)
            snap = self.snapshot()
            return {
                "ok": True,
                "command": ", ".join(c.name for c in commands),
                "durationMs": int((time.monotonic() - started) * 1000),
                "logs": logs,
                "snapshot": snap,
            }
        except Exception as exc:  # noqa: BLE001
            snap = None
            with suppress(Exception):
                snap = self.snapshot()
            return {
                "ok": False,
                "command": snippet.splitlines()[0],
                "durationMs": int((time.monotonic() - started) * 1000),
                "error": str(exc),
                "logs": logs,
                "snapshot": snap,
            }
        finally:
            self.runner.echo = original_echo
            self.runner._env = original_env

    def validate_yaml(self, params: dict[str, Any]) -> dict[str, Any]:
        yaml_text = params.get("yaml") or ""
        flow_path_raw = (params.get("flowPath") or "").strip()
        flow_path = self._resolve_flow_path(params)
        if not yaml_text.strip():
            return {
                "valid": False,
                "tags": [],
                "steps": 0,
                "error": "Flow YAML is empty.",
            }

        with tempfile.TemporaryDirectory(prefix="pyestro-validate-") as tmp:
            target_file = Path(tmp) / "flow.yaml"
            target_file.write_text(yaml_text, encoding="utf-8")
            header, raw_commands = self.flow_mod.read_flow_file(target_file)
            config = self.flow_mod.parse_config(header)
            dummy_driver = self._dummy_driver()
            runner = self._make_runner(dummy_driver, params, flow_path)
            build_params = inspect.signature(runner._build_env).parameters
            if "flow_path" in build_params:
                env = runner._build_env(config, flow_path)
            else:
                env = runner._build_env(config)
            commands = self.flow_mod.compile_commands(raw_commands, env)
            self.flow_mod.compile_commands(config.on_start, env, "onFlowStart › ")
            self.flow_mod.compile_commands(config.on_complete, env, "onFlowComplete › ")
            return {
                "valid": True,
                "appId": config.app_id or "",
                "name": config.name or "",
                "url": getattr(config, "url", None) or "",
                "tags": list(getattr(config, "tags", ()) or ()),
                "steps": len(commands),
                "engine": self.pkg_name,
                "flowPath": flow_path_raw,
            }

    def run_flow_yaml(self, params: dict[str, Any]) -> dict[str, Any]:
        yaml_text = params.get("yaml") or ""
        flow_path_raw = (params.get("flowPath") or "").strip()
        flow_path = self._resolve_flow_path(params)
        record = bool(params.get("record", False))
        report = bool(params.get("report", False))

        logs: list[str] = []

        def stream_echo(msg: str) -> None:
            text = str(msg)
            logs.append(text)
            _emit_log(text)
            if text.lstrip().startswith(("PASS ", "FAIL ")):
                with suppress(Exception):
                    _emit_snapshot(self.snapshot_for(driver))

        artifacts_dir = (
            flow_path.parent / "artifacts"
            if flow_path is not None
            else _default_artifacts_dir()
        )

        # Reuse active session driver when already connected, otherwise create a temporary one.
        owns_driver = self.driver is None
        driver = self.driver
        if owns_driver:
            if self.target == "web":
                web_mod = importlib.import_module("pyestro_web.web")
                url = _normalize_url(params.get("url") or "") or None
                browser = (params.get("browser") or "chromium").strip().lower()
                viewport = _parse_viewport(params.get("viewport") or "mobile")
                headless = bool(params.get("headless", True))
                driver = web_mod.PlaywrightDriver(
                    browser_name=browser,
                    headless=headless,
                    viewport=viewport,
                    base_url=url,
                )
                driver.start()
            else:
                android_mod = importlib.import_module("pyestro.android")
                serial = (params.get("serial") or "").strip() or None
                adb_path = (params.get("adbPath") or "adb").strip() or "adb"
                driver = android_mod.AndroidDriver(serial=serial, adb_path=adb_path)

        base_url = _normalize_url(params.get("url") or "") or None
        kwargs: dict[str, Any] = {
            "artifacts_dir": artifacts_dir,
            "echo": stream_echo,
        }
        if self.target == "web" and base_url:
            kwargs["base_url"] = base_url

        runner = self._make_runner(driver, params, flow_path, **kwargs)
        started = time.monotonic()

        tmp_ctx: tempfile.TemporaryDirectory[str] | None = None
        try:
            if flow_path_raw:
                run_path = (
                    Path(flow_path_raw).resolve().parent
                    / f".pyestro-studio-run-{os.getpid()}.yaml"
                )
                run_path.write_text(yaml_text, encoding="utf-8")
                cleanup_path: Path | None = run_path
            else:
                tmp_ctx = tempfile.TemporaryDirectory(prefix="pyestro-run-")
                run_path = Path(tmp_ctx.name) / "studio_flow.yaml"
                run_path.write_text(yaml_text, encoding="utf-8")
                cleanup_path = None

            try:
                result = runner.run(run_path, record=record)
            finally:
                if cleanup_path is not None and cleanup_path.exists():
                    cleanup_path.unlink(missing_ok=True)

            report_path = ""
            if report:
                reports_mod = importlib.import_module(f"{self.pkg_name}.reports")
                html_out = artifacts_dir / "report.html"
                junit_out = artifacts_dir / "report.xml"
                reports_mod.generate_html_report([result], html_out)
                reports_mod.generate_junit_xml([result], junit_out)
                report_path = str(html_out)

            snap = None
            if not owns_driver:
                if self.runner is not None:
                    self.runner._env = dict(getattr(runner, "_env", {}) or {})
                with suppress(Exception):
                    snap = self.snapshot()

            return {
                "ok": bool(result.ok),
                "passed": int(result.passed),
                "failed": int(result.failed),
                "skipped": int(result.skipped),
                "total": int(result.total),
                "durationMs": int((time.monotonic() - started) * 1000),
                "logs": logs,
                "reportPath": report_path,
                "videoPath": str(result.video_path) if result.video_path else "",
                "snapshot": snap,
            }
        finally:
            if tmp_ctx is not None:
                tmp_ctx.cleanup()
            if owns_driver and driver is not None and hasattr(driver, "close"):
                with suppress(Exception):
                    driver.close()

    def _dummy_driver(self) -> Any:
        if self.target == "web":
            web_mod = importlib.import_module("pyestro_web.web")
            return web_mod.PlaywrightDriver()
        android_mod = importlib.import_module("pyestro.android")
        return android_mod.AndroidDriver()


def main() -> None:
    session = StudioSession(TARGET)
    for raw_line in sys.stdin:
        line = raw_line.strip()
        if not line:
            continue
        req_id: Any = None
        try:
            req = json.loads(line)
            req_id = req.get("id")
            method = req.get("method")
            params = req.get("params") or {}

            if method == "ping":
                res = session.ping()
            elif method == "start_session":
                res = session.start_session(params)
            elif method == "stop_session":
                session.close()
                res = {"connected": False}
            elif method == "snapshot":
                res = session.snapshot()
            elif method == "run_step":
                res = session.run_step(params)
            elif method == "validate_yaml":
                res = session.validate_yaml(params)
            elif method == "run_flow_yaml":
                res = session.run_flow_yaml(params)
            elif method == "shutdown":
                session.close()
                sys.stdout.write(
                    json.dumps({"id": req_id, "result": {"ok": True}}) + "\n"
                )
                sys.stdout.flush()
                return
            else:
                raise ValueError(f"Unknown studio worker method: {method}")

            sys.stdout.write(
                json.dumps({"id": req_id, "result": res}, ensure_ascii=False) + "\n"
            )
            sys.stdout.flush()
        except Exception as exc:  # noqa: BLE001
            err_msg = str(exc) or type(exc).__name__
            sys.stdout.write(
                json.dumps(
                    {
                        "id": req_id,
                        "error": err_msg,
                        "trace": traceback.format_exc(),
                    },
                    ensure_ascii=False,
                )
                + "\n"
            )
            sys.stdout.flush()


if __name__ == "__main__":
    main()
