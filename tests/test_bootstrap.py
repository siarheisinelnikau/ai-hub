"""First-launch configuration must persist safely and preserve existing access."""
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
import env
import start


class BootstrapTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.cwd = Path.cwd()
        self.addCleanup(os.chdir, self.cwd)
        self.addCleanup(patch.stopall)
        # No inherited credentials, host settings or real files may influence tests.
        patch.dict(os.environ, {}, clear=True).start()
        patch.object(env, "ROOT", self.root).start()
        patch.object(start, "ROOT", self.root).start()
        patch.object(sys.stdin, "isatty", return_value=True).start()
        patch.object(start.subprocess, "run").start()
        patch("builtins.print").start()
        (self.root / ".env.example").write_text("AIHUB_TOKEN=\nAIHUB_SERVER_IP=127.0.0.1\n", encoding="utf-8")

    def saved_token(self):
        text = (self.root / ".env").read_text(encoding="utf-8")
        value = next(line.split("=", 1)[1] for line in text.splitlines() if line.startswith("AIHUB_TOKEN="))
        return json.loads(value)

    def test_custom_token_is_private_and_literal(self):
        token = 'private # $literal "quoted" \\ value'
        with patch.object(start.getpass, "getpass", side_effect=[token, token]):
            start.main()
        self.assertEqual(self.saved_token(), token)
        self.assertEqual(os.environ["AIHUB_TOKEN"], token)
        with patch.object(start.getpass, "getpass", side_effect=AssertionError("prompted twice")):
            start.main()

    def test_blank_input_generates_and_persists_private_token(self):
        with patch.object(start.getpass, "getpass", return_value=""):
            start.main()
        self.assertEqual(len(self.saved_token()), 32)
        self.assertNotEqual(self.saved_token(), "dev-token-123")

    def test_confirmation_mismatch_does_not_save(self):
        with patch.object(start.getpass, "getpass", side_effect=["private-one", "private-two"]):
            with self.assertRaises(ValueError):
                start.main()
        self.assertEqual((self.root / ".env").read_text(encoding="utf-8"), (self.root / ".env.example").read_text(encoding="utf-8"))

    def test_existing_file_token_is_preserved_without_prompt(self):
        (self.root / "token").write_text("existing-private-token", encoding="utf-8")
        with patch.object(start.getpass, "getpass", side_effect=AssertionError("unnecessary prompt")):
            start.main()
        self.assertEqual((self.root / "token").read_text(encoding="utf-8"), "existing-private-token")


if __name__ == "__main__":
    unittest.main()
