"""Exercise the real installer with isolated release downloads and destinations."""

import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().parents[1] / "install.sh"
PAYLOAD = b'#!/bin/sh\nprintf "sshm 0.3.0\\n"\n'
OLD = b'#!/bin/sh\nprintf "sshm 0.2.0\\n"\n'


class InstallerTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="sshm-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.home = self.root / "home"
        self.prefix = self.home / "custom prefix"
        self.binary = self.prefix / "bin/sshm"
        self.downloads = self.root / "downloads"
        self.downloads.mkdir()
        self.mocks = self.root / "mocks"
        self.mocks.mkdir()
        self.payload = self.root / "payload"
        self.payload.write_bytes(PAYLOAD)
        self.env = dict(
            os.environ, HOME=str(self.home), PREFIX=str(self.prefix),
            PATH=str(self.mocks) + os.pathsep + os.environ["PATH"],
            TMPDIR=str(self.downloads), MODE="ok", MOCK_OS="Linux",
            MOCK_ARCH="x86_64", PAYLOAD=str(self.payload),
            HASH=hashlib.sha256(PAYLOAD).hexdigest(),
        )
        self.mock("uname", """
import os, sys
print(os.environ["MOCK_OS" if sys.argv[1] == "-s" else "MOCK_ARCH"])
""")
        self.mock("curl", """
import os, pathlib, shutil, sys
url, dest = sys.argv[2], pathlib.Path(sys.argv[4])
mode = os.environ["MODE"]
if mode == "download-failure":
    sys.exit(22)
if url.endswith("/SHA256SUMS"):
    checksum = "0" * 64 if mode == "mismatch" else os.environ["HASH"]
    assets = ["other"] if mode == "missing" else [
        "sshm-" + system + "-" + arch
        for system in ("linux", "darwin") for arch in ("amd64", "arm64")
    ]
    dest.write_text("".join(checksum + "  " + asset + "\\n" for asset in assets))
else:
    shutil.copyfile(os.environ["PAYLOAD"], dest)
""")

    def mock(self, name, code):
        path = self.mocks / name
        path.write_text("#!" + sys.executable + "\n" + code)
        path.chmod(0o755)

    def old_install(self):
        self.binary.parent.mkdir(parents=True, exist_ok=True)
        self.binary.write_bytes(OLD)
        self.binary.chmod(0o755)

    def run_installer(self, action=None, **env):
        # Piped stdin reproduces the documented curl | bash interface.
        args = ["bash", "-s", "--"]
        if action:
            args.append(action)
        return subprocess.run(
            args, input=SCRIPT.read_bytes(), env=dict(self.env, **env),
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15,
        )

    def assert_clean(self):
        self.assertEqual(list(self.downloads.iterdir()), [])
        self.assertEqual(list(self.binary.parent.glob(".sshm.*")), [])

    def test_install_and_update(self):
        for action in (None, "update"):
            result = self.run_installer(action)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(self.binary.read_bytes(), PAYLOAD)
            self.assertTrue(os.access(self.binary, os.X_OK))
            self.assert_clean()

    def test_download_and_checksum_failures_preserve_binary(self):
        self.old_install()
        for mode in ("download-failure", "mismatch", "missing"):
            with self.subTest(mode=mode):
                result = self.run_installer("update", MODE=mode)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(self.binary.read_bytes(), OLD)
                self.assert_clean()

    def test_copy_failure_and_signal_preserve_binary(self):
        real_install = shutil.which("install")
        self.mock("install", f"""
import os, pathlib, signal, sys
if sys.argv[1] == "-m":
    pathlib.Path(sys.argv[-1]).write_bytes(b"partial")
    if os.environ["MODE"] == "signal":
        os.kill(os.getppid(), signal.SIGTERM)
    sys.exit(1)
os.execv({real_install!r}, [{real_install!r}] + sys.argv[1:])
""")
        self.old_install()
        for mode in ("copy-failure", "signal"):
            with self.subTest(mode=mode):
                result = self.run_installer("update", MODE=mode)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(self.binary.read_bytes(), OLD)
                self.assert_clean()

    def test_rename_failure_preserves_binary(self):
        self.mock("mv", "import sys\nsys.exit(1)\n")
        self.old_install()
        self.assertNotEqual(self.run_installer("update").returncode, 0)
        self.assertEqual(self.binary.read_bytes(), OLD)
        self.assert_clean()

    def test_directory_destination_is_rejected(self):
        self.binary.mkdir(parents=True)
        self.assertNotEqual(self.run_installer().returncode, 0)
        self.assertEqual(list(self.binary.iterdir()), [])
        self.assert_clean()

    def test_uninstall_preserves_user_data_and_is_repeatable(self):
        self.old_install()
        files = [self.home / name for name in (
            ".config/sshm/state.json", ".local/state/sshm/backups/config",
            ".ssh/config", ".ssh/id_ed25519",
        )]
        for path in files:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("keep")
        for _ in range(2):
            self.assertEqual(self.run_installer("uninstall").returncode, 0)
            self.assertFalse(self.binary.exists())
            for path in files:
                self.assertEqual(path.read_text(), "keep")

    def test_unsupported_platform_preserves_binary(self):
        self.old_install()
        for env in ({"MOCK_OS": "FreeBSD"}, {"MOCK_ARCH": "riscv64"}):
            self.assertNotEqual(self.run_installer("update", **env).returncode, 0)
            self.assertEqual(self.binary.read_bytes(), OLD)
            self.assert_clean()

    def test_platform_variants(self):
        for system in ("Linux", "Darwin"):
            for arch in ("x86_64", "aarch64"):
                with self.subTest(system=system, arch=arch):
                    result = self.run_installer(MOCK_OS=system, MOCK_ARCH=arch)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertEqual(self.binary.read_bytes(), PAYLOAD)
                    self.assert_clean()

    def test_update_without_install_is_rejected(self):
        self.assertNotEqual(self.run_installer("update").returncode, 0)
        self.assertFalse(self.binary.exists())

    def test_invalid_action_is_rejected(self):
        self.assertEqual(self.run_installer("invalid").returncode, 2)
        self.assertFalse(self.binary.exists())


if __name__ == "__main__":
    unittest.main()
