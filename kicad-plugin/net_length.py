"""KiCad action: how far the selected net is from its length target."""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from trace_length_analyzer.app import net_length  # noqa: E402

if __name__ == "__main__":
    sys.exit(net_length())
