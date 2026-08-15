"""Black-box tests for the locally compiled SoloAPI Image2 Windows helper.

These tests use a localhost mock and never contact or bill a real upstream.
"""

from __future__ import annotations

import base64
import json
import os
from pathlib import Path
import subprocess
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import unittest


ROOT = Path(__file__).resolve().parents[1]
EXE = ROOT / "skills" / "soloapi-image2" / "scripts" / "bin" / "soloapi-image2-windows-amd64.exe"
FAKE_KEY = "sk-local-mock-never-real"
PNG = base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
)


class MockState:
    requests: list[dict[str, object]]

    def __init__(self) -> None:
        self.requests = []


class MockHandler(BaseHTTPRequestHandler):
    server: "MockServer"

    def log_message(self, *_: object) -> None:
        return

    def do_POST(self) -> None:  # noqa: N802
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length)
        self.server.state.requests.append(
            {
                "path": self.path,
                "authorization": self.headers.get("Authorization"),
                "content_type": self.headers.get("Content-Type"),
                "body": body,
            }
        )
        payload = json.dumps(
            {"data": [{"b64_json": base64.b64encode(PNG).decode("ascii")}]} 
        ).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.send_header("x-request-id", "req-local-e2e")
        self.end_headers()
        self.wfile.write(payload)


class MockServer(ThreadingHTTPServer):
    state: MockState


class SoloAPIImage2E2E(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        if not EXE.exists():
            raise unittest.SkipTest(f"compiled helper not found: {EXE}")
        cls.state = MockState()
        cls.server = MockServer(("127.0.0.1", 0), MockHandler)
        cls.server.state = cls.state
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        cls.base_url = f"http://127.0.0.1:{cls.server.server_port}/v1"

    @classmethod
    def tearDownClass(cls) -> None:
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join(timeout=5)

    def setUp(self) -> None:
        self.state.requests.clear()
        self.tempdir = tempfile.TemporaryDirectory(prefix="soloapi-image2-e2e-")
        self.addCleanup(self.tempdir.cleanup)
        self.env = os.environ.copy()
        self.env["SOLOAPI_IMAGE2_BASE_URL"] = self.base_url
        self.env["SOLOAPI_IMAGE2_API_KEY"] = FAKE_KEY

    def run_helper(self, *args: str, expect_ok: bool = True) -> subprocess.CompletedProcess[str]:
        process = subprocess.run(
            [str(EXE), *args],
            env=self.env,
            text=True,
            encoding="utf-8",
            capture_output=True,
            timeout=20,
            check=False,
        )
        combined = process.stdout + process.stderr
        self.assertNotIn(FAKE_KEY, combined, "helper leaked the API key")
        if expect_ok:
            self.assertEqual(process.returncode, 0, combined)
        return process

    def test_check_and_dry_run_make_no_request(self) -> None:
        checked = self.run_helper("check")
        result = json.loads(checked.stdout)
        self.assertTrue(result["key_configured"])
        self.assertFalse(result["network_called"])

        out = Path(self.tempdir.name) / "dry-run.png"
        dry = self.run_helper(
            "generate", "--prompt", "中文提示词", "--out", str(out), "--dry-run"
        )
        self.assertTrue(json.loads(dry.stdout)["dry_run"])
        self.assertFalse(out.exists())
        self.assertEqual(self.state.requests, [])

    def test_generate_request_and_output(self) -> None:
        out = Path(self.tempdir.name) / "generated.png"
        process = self.run_helper(
            "generate",
            "--prompt",
            "蓝绿色纸雕灯塔",
            "--size",
            "1024x1024",
            "--out",
            str(out),
            "--yes",
        )
        result = json.loads(process.stdout)
        self.assertEqual(result["paid_attempts"], 1)
        self.assertEqual(result["request_id"], "req-local-e2e")
        self.assertEqual(out.read_bytes(), PNG)
        self.assertEqual(len(self.state.requests), 1)
        request = self.state.requests[0]
        self.assertEqual(request["path"], "/v1/images/generations")
        self.assertEqual(request["authorization"], f"Bearer {FAKE_KEY}")
        body = json.loads(request["body"].decode("utf-8"))
        self.assertEqual(body["model"], "gpt-image-2")
        self.assertEqual(body["n"], 1)
        self.assertEqual(body["size"], "1024x1024")

    def test_reference_edit_request_and_output(self) -> None:
        reference = Path(self.tempdir.name) / "reference.png"
        reference.write_bytes(PNG)
        out = Path(self.tempdir.name) / "edited.png"
        process = self.run_helper(
            "edit",
            "--prompt",
            "保留构图，改为木刻风格",
            "--image",
            str(reference),
            "--out",
            str(out),
            "--yes",
        )
        result = json.loads(process.stdout)
        self.assertEqual(result["reference_count"], 1)
        self.assertEqual(out.read_bytes(), PNG)
        self.assertEqual(len(self.state.requests), 1)
        request = self.state.requests[0]
        self.assertEqual(request["path"], "/v1/images/edits")
        self.assertEqual(request["authorization"], f"Bearer {FAKE_KEY}")
        self.assertIn(b'name="image"', request["body"])
        self.assertIn(b'name="model"', request["body"])
        self.assertIn(b"gpt-image-2", request["body"])

    def test_missing_confirmation_is_blocked(self) -> None:
        out = Path(self.tempdir.name) / "blocked.png"
        process = self.run_helper(
            "generate", "--prompt", "test", "--out", str(out), expect_ok=False
        )
        self.assertNotEqual(process.returncode, 0)
        self.assertIn("require --yes", process.stderr)
        self.assertFalse(out.exists())
        self.assertEqual(self.state.requests, [])


if __name__ == "__main__":
    unittest.main(verbosity=2)
