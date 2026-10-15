"""The command line: ``python -m oracle run`` and ``python -m oracle generate``."""

import json
import subprocess
import sys

from builders import journal, line, scenario

from oracle.scenario import check_result, load_scenario


def _oracle(*args, cwd=None):
    return subprocess.run([sys.executable, "-m", "oracle", *args], capture_output=True, text=True, cwd=cwd, check=False)


# req: QA-ENG-01
def test_run_writes_the_result_to_stdout(tmp_path):
    path = tmp_path / "s.json"
    path.write_text(
        json.dumps(
            scenario(
                journal(
                    "JNL-0001", "2026-01-05", line("1010", "USD", "100.00", "1.3472"), line("4010", "SGD", "-134.72")
                ),
                journal("JNL-0002", "2026-01-05", line("1010", "SGD", "1.00")),
            )
        )
    )
    done = _oracle("run", str(path), "--explain", cwd=tmp_path)  # from any directory
    assert done.returncode == 0, done.stderr
    result = json.loads(done.stdout)
    check_result(result)
    assert result["trial_balance"] == {"2026-01": {"1010": "134.72", "4010": "-134.72"}}
    assert result["rejected"] == ["JNL-0002"]
    assert "rejected JNL-0002: fewer-than-two-lines" in done.stderr


# req: QA-ENG-01
def test_run_refuses_an_invalid_scenario_with_status_2(tmp_path):
    path = tmp_path / "bad.json"
    path.write_text('{"schema": "fyniq-scenario@1"}')
    done = _oracle("run", str(path))
    assert done.returncode == 2
    assert done.stdout == ""
    assert "required property" in done.stderr
    missing = _oracle("run", str(tmp_path / "missing.json"))
    assert missing.returncode == 2 and "cannot read" in missing.stderr


# req: QA-ENG-01
def test_generate_writes_named_scenarios_that_the_oracle_reads(tmp_path):
    out = tmp_path / "generated"
    done = _oracle("generate", "--seed", "5", "--count", "12", "--out", str(out))
    assert done.returncode == 0, done.stderr
    names = sorted(p.name for p in out.iterdir())
    assert names == [f"gen-5-{i:04d}.json" for i in range(12)]
    for path in out.iterdir():
        assert load_scenario(path).name == path.stem


# req: QA-ENG-01
def test_generate_refuses_a_count_below_one(tmp_path):
    done = _oracle("generate", "--seed", "1", "--count", "0", "--out", str(tmp_path))
    assert done.returncode == 2
