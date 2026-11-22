"""Persistent JSON-RPC sidecar bridge between Pyestro Studio (Go/Glaze) and pyestro / pyestro-web."""

from __future__ import annotations

import base64
import importlib
import json
import os
import re
import sys
import tempfile
import time
import traceback
from pathlib import Path
from typing import Any

import yaml

TARGET = os.environ.get("PYESTRO_STUDIO_TARGET", "android").strip().lower()


def _emit_log(message: str) -> None:
    payload = {"event": "log", "message": str(message)}
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
            try:
                self.driver.close()
            except Exception:
                pass
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
            url = (params.get("url") or "http://localhost:8081").strip()
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

        with tempfile.TemporaryDirectory(prefix="pyestro-studio-") as tmp:
            png_path = Path(tmp) / "screen.png"
            self.driver.screenshot(png_path)
            img_b64 = base64.b64encode(png_path.read_bytes()).decode("ascii")

        width, height = self.driver.screen_size()
        current_url = ""
        if self.target == "web":
            raw_elements = self.driver.dump_elements()
            current_url = self.driver.current_url()
        else:
            root = self.driver.dump_hierarchy()
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
        if isinstance(parsed, dict):
            raw_commands = [parsed]
        elif isinstance(parsed, str):
            raw_commands = [parsed]
        elif isinstance(parsed, list):
            raw_commands = parsed
        else:
            raise ValueError(
                "Command snippet must be a YAML command or list of commands."
            )

        logs: list[str] = []
        original_echo = self.runner.echo
        self.runner.echo = lambda msg: (logs.append(str(msg)), _emit_log(msg))

        started = time.monotonic()
        try:
            env = dict(getattr(self.runner, "_env", {}) or {})
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
        except Exception as exc:
            snap = None
            try:
                snap = self.snapshot()
            except Exception:
                pass
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

    def validate_yaml(self, params: dict[str, Any]) -> dict[str, Any]:
        yaml_text = params.get("yaml") or ""
        flow_path = params.get("flowPath") or ""
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
            runner = self.runner_mod.FlowRunner(dummy_driver)
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
                "flowPath": flow_path,
            }

    def run_flow_yaml(self, params: dict[str, Any]) -> dict[str, Any]:
        yaml_text = params.get("yaml") or ""
        flow_path_raw = (params.get("flowPath") or "").strip()
        record = bool(params.get("record", False))
        report = bool(params.get("report", False))

        logs: list[str] = []

        def stream_echo(msg: str) -> None:
            logs.append(str(msg))
            _emit_log(msg)

        artifacts_dir = (
            Path(flow_path_raw).resolve().parent / "artifacts"
            if flow_path_raw
            else Path.cwd() / "artifacts"
        )

        # Reuse active session driver when already connected, otherwise create a temporary one.
        owns_driver = self.driver is None
        driver = self.driver
        if owns_driver:
            if self.target == "web":
                web_mod = importlib.import_module("pyestro_web.web")
                url = (params.get("url") or "").strip() or None
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

        base_url = (params.get("url") or "").strip() or None
        kwargs: dict[str, Any] = {
            "artifacts_dir": artifacts_dir,
            "echo": stream_echo,
        }
        if self.target == "web" and base_url:
            kwargs["base_url"] = base_url

        runner = self.runner_mod.FlowRunner(driver, **kwargs)
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
                try:
                    snap = self.snapshot()
                except Exception:
                    pass

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
                try:
                    driver.close()
                except Exception:
                    pass

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
        except Exception as exc:
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
