import sys
from pathlib import Path

import pytest

PLUGIN = Path(__file__).resolve().parent.parent
REPO = PLUGIN.parent
DEMO = REPO / "demo-pcb"

sys.path.insert(0, str(PLUGIN))


@pytest.fixture(scope="session")
def engine_path():
    from trace_length_analyzer.engine import EngineError, find_engine

    try:
        return find_engine(PLUGIN)
    except EngineError as e:
        pytest.skip(f"engine not built (make build): {e}")


@pytest.fixture(scope="session")
def demo_files():
    board = DEMO / "ai-vision.kicad_pcb"
    if not board.is_file():
        pytest.skip("demo board missing")
    return {
        "text": board.read_bytes(),
        "filename": board.name,
        "project": (DEMO / "ai-vision.kicad_pro").read_bytes(),
        "rules": (DEMO / "ai-vision.kicad_dru").read_bytes(),
    }


@pytest.fixture
def engine(engine_path):
    from trace_length_analyzer.engine import Engine

    e = Engine(engine_path)
    yield e
    e.close()
