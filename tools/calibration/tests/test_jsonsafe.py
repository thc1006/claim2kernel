import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from jsonsafe import load_json


class StrictJSONTest(unittest.TestCase):
    def test_duplicate_key_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "bad.json"
            path.write_text('{"x":1,"x":2}')
            with self.assertRaisesRegex(ValueError, "duplicate"):
                load_json(path)

    def test_nonstandard_number_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "bad.json"
            path.write_text('{"x":NaN}')
            with self.assertRaisesRegex(ValueError, "forbidden"):
                load_json(path)


if __name__ == "__main__":
    unittest.main()
