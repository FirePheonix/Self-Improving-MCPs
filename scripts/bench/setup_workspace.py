"""Create the small File System fixture the benchmark executes real calls against.

    python scripts/bench/setup_workspace.py [target]   # default: ../evaluation/workspace/test_project_root

The MCPToolBench++ File System tasks use relative paths like ./test_project_root/data/... . This fixture is hand-written and
does not contain every file or text the dataset's edit tasks expect, so even the dataset's own reference calls succeed only
201 of 241 times against it. Execution counts are therefore for comparing arms, not absolute scores. The bench copies the
fixture to a temporary folder for each arm, so runs never modify it.
"""
import sys
from pathlib import Path

target = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).resolve().parents[2].parent / "evaluation" / "workspace" / "test_project_root"
files = {
    "data/test_file_csv_1.csv": "a,b\n1,2\n",
    "data/test_file_txt_1.txt": "hello txt1",
    "data/test_file_json_1.json": '{"ok": true}',
    "data/test_file_json_2.json": '{"n": 2}',
    "docs/README.md": "# docs\n",
    "src/main.py": "print(1)\n",
    "src/config/settings.yaml": "debug: true\n",
    "src/utils/file_utils.py": "def f():\n    pass\n",
    "tests/unit/test_calculations.py": "def test_ok():\n    assert True\n",
    "requirements.txt": "pytest\n",
    "config/app.yaml": "name: demo\n",
}
for rel, content in files.items():
    p = target / rel
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(content, encoding="utf-8")
print(target)
