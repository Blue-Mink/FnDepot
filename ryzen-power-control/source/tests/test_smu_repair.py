import importlib.util
import sys
import tempfile
import threading
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("powerd", Path(__file__).resolve().parents[1] / "app/server/powerd.py")
powerd = importlib.util.module_from_spec(spec)
spec.loader.exec_module(powerd)

INFO = "CPU Family: Hawk Point\n" + "\n".join(
    "| %s | %s |" % (name, value)
    for name, value in {
        "STAPM LIMIT": 45, "PPT LIMIT FAST": 45, "PPT LIMIT SLOW": 45,
        "STAPM VALUE": 5, "PPT VALUE FAST": 8, "PPT VALUE SLOW": 5,
        "THM VALUE CORE": 40,
    }.items()
)
SMU_ERROR = powerd.PowerError("requested ryzen_smu backend is unavailable or incompatible")
MEM_ERROR = powerd.PowerError("CPU Family: Hawk Point\nUnable to get memory access")


class SmuRepairTests(unittest.TestCase):
    def app(self, replies):
        app = powerd.PowerApp.__new__(powerd.PowerApp)
        app.io_lock = threading.Lock()
        app.smu_repair_attempted = False
        app.smu_repair_error = None
        app.ryzenadj_backend = "smu"
        sequence = iter(replies)
        app.read_backends = []
        def read(args):
            app.read_backends.append(app.ryzenadj_backend)
            reply = next(sequence)
            if isinstance(reply, Exception):
                raise reply
            return reply
        app._run = Mock(side_effect=read)
        app._repair_smu_driver = Mock()
        app._log_command = Mock()
        return app

    def test_healthy_driver_does_not_build_or_use_mem(self):
        app = self.app([INFO])
        self.assertEqual(app._read_info()["limits"]["fast"], 45)
        app._repair_smu_driver.assert_not_called()
        self.assertEqual(app.read_backends, ["smu"])

    def test_missing_driver_repairs_before_mem(self):
        app = self.app([SMU_ERROR, INFO, INFO])
        app._read_info()
        app._read_info()
        app._repair_smu_driver.assert_called_once_with()
        self.assertEqual(app.read_backends, ["smu", "smu", "smu"])

    def test_build_failure_falls_back_once_and_logs_reason(self):
        app = self.app([SMU_ERROR, INFO, INFO])
        app._repair_smu_driver.side_effect = powerd.PowerError("缺少内核构建文件")
        app._read_info()
        app._read_info()
        self.assertEqual(app.read_backends, ["smu", "mem", "mem"])
        app._repair_smu_driver.assert_called_once_with()
        self.assertIn("缺少内核构建文件", app.smu_repair_error)
        self.assertTrue(any("error" in c.kwargs for c in app._log_command.call_args_list))

    def test_loaded_but_broken_driver_tries_repair_then_mem(self):
        app = self.app([SMU_ERROR, powerd.PowerError("refresh_table failed"), INFO])
        app._read_info()
        self.assertEqual(app.read_backends, ["smu", "smu", "mem"])
        self.assertIn("refresh_table failed", app.smu_repair_error)

    def test_invalid_repaired_table_is_not_success(self):
        app = self.app([SMU_ERROR, "CPU Family: Hawk Point", INFO])
        app._read_info()
        self.assertEqual(app.read_backends, ["smu", "smu", "mem"])
        self.assertIn("缺少字段", app.smu_repair_error)

    def test_both_backends_fail_and_repair_is_not_repeated(self):
        app = self.app([SMU_ERROR, MEM_ERROR, MEM_ERROR])
        app._repair_smu_driver.side_effect = powerd.PowerError("驱动加载被拒绝")
        for _ in range(2):
            with self.assertRaisesRegex(powerd.PowerError, "驱动加载被拒绝.*回退读取也失败"):
                app._read_info()
        app._repair_smu_driver.assert_called_once_with()
        self.assertEqual(app.read_backends, ["smu", "mem", "mem"])

    def test_driver_fails_again_after_success_uses_mem_without_rebuilding(self):
        app = self.app([SMU_ERROR, INFO, SMU_ERROR, INFO])
        app._read_info()
        app._read_info()
        app._repair_smu_driver.assert_called_once_with()
        self.assertEqual(app.read_backends, ["smu", "smu", "smu", "mem"])

    def test_reads_and_writes_use_selected_backend(self):
        app = self.app([])
        del app._run
        app.ryzenadj = "/app/ryzenadj"
        app.ryzenadj_backend = "mem"
        app._run_command = Mock()
        app._run(["--stapm-limit=45000"])
        self.assertEqual(app._run_command.call_args.kwargs["env"]["RYZENADJ_BACKEND"], "mem")

    def test_runner_keeps_stdout_failure_and_stderr_warning(self):
        app = self.app([])
        result = Mock(returncode=251, stdout="CPU Family: Hawk Point\nUnable to get memory access", stderr="fallback to /dev/mem")
        with patch.object(powerd.subprocess, "run", return_value=result):
            with self.assertRaises(powerd.PowerError) as caught:
                app._run_command(["ryzenadj", "--info"])
        self.assertIn("Unable to get memory access", str(caught.exception))
        self.assertIn("fallback to /dev/mem", str(caught.exception))

    def test_builder_replaces_loaded_module_after_successful_build(self):
        with tempfile.TemporaryDirectory() as directory:
            app = powerd.PowerApp.__new__(powerd.PowerApp)
            app.state_dir = Path(directory)
            app._run_command = Mock()
            with patch.object(powerd.os, "geteuid", return_value=0), \
                 patch.object(Path, "exists", return_value=True), \
                 patch.object(Path, "is_file", return_value=True), \
                 patch.object(powerd.shutil, "which", side_effect=lambda name: "/usr/bin/" + name):
                app._repair_smu_driver()
            calls = app._run_command.call_args_list
            self.assertEqual(len(calls), 3)
            self.assertIn("modules", calls[0].args[0])
            self.assertEqual(calls[1].args[0], ["/usr/bin/rmmod", "ryzen_smu"])
            self.assertEqual(calls[2].args[0][0], "/usr/bin/insmod")


if __name__ == "__main__":
    unittest.main()
