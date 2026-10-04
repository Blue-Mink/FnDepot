import tempfile
import threading
import unittest
from pathlib import Path
from unittest.mock import patch

from test_smu_repair import powerd


class LogCleanupTests(unittest.TestCase):
    def test_manual_and_daily_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            app = powerd.PowerApp.__new__(powerd.PowerApp)
            app.state_dir = Path(directory)
            app.ryzenadj_log_path = app.state_dir / "ryzenadj.log"
            app.ryzenadj_log_lock = threading.Lock()
            app.log_clear_deadline = 86400
            service_log = app.state_dir / "info.log"
            state = app.state_dir / "control.json"
            state.write_text('{"enabled": true}')
            for path in (app.ryzenadj_log_path, service_log):
                path.write_text("old log")
            with patch.object(powerd.time, "monotonic", return_value=100):
                self.assertFalse(app.clear_logs(only_if_due=True)["cleared"])
                self.assertEqual(app.ryzenadj_log_path.read_text(), "old log")
                self.assertTrue(app.clear_logs()["cleared"])
                self.assertEqual(app.log_clear_deadline, 86500)
            self.assertEqual(service_log.read_text(), "")
            self.assertEqual(state.read_text(), '{"enabled": true}')
            app._log_command(["ryzenadj", "--info"], stdout="new log")
            self.assertIn("new log", app.ryzenadj_log_path.read_text())
            with patch.object(powerd.time, "monotonic", return_value=86500):
                self.assertTrue(app.clear_logs(only_if_due=True)["cleared"])
                self.assertEqual(app.ryzenadj_log_path.read_text(), "")
            with patch.object(Path, "write_text", side_effect=PermissionError("denied")):
                with self.assertRaisesRegex(powerd.PowerError, "清理应用日志失败"):
                    app.clear_logs()


if __name__ == "__main__":
    unittest.main()
