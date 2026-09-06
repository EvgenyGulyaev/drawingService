"""Run on Linux: python3 deploy/test-deploy.py. Uses only a temporary directory."""
import os
from pathlib import Path
import subprocess
import tempfile


with tempfile.TemporaryDirectory(prefix="crocodile-deploy-test-") as directory:
    base = Path(directory)
    initial = base / "releases" / "initial"
    (initial / "web").mkdir(parents=True)
    (initial / "web" / "index.html").write_text("old web")
    (initial / "crocodile").write_text("old api")
    (base / "current").symlink_to(initial)
    script = Path(__file__).with_name("deploy-crocodile.sh").read_text()
    script = script.replace("base=/opt/crocodile", f"base={base}")
    mocks = r'''
sudo() { echo restart >> "$TEST_BASE/restarts"; }
systemctl() { return 0; }
sleep() { :; }
curl() {
    if [[ $* == *NOTFOUND* ]]; then
        if [[ ${TEST_FAIL:-} == api ]]; then printf '500'; else printf '404'; fi
    elif [[ ${TEST_FAIL:-} == web ]]; then
        printf 'wrong response'
    else
        cat "$TEST_BASE/current/web/index.html"
    fi
}
'''

    def deploy(component, number, fail=""):
        release_id = f"{number}-1-{'a' * 40}"
        incoming = base / "incoming" / f"{component}-{release_id}"
        incoming.mkdir(parents=True)
        filename = "crocodile" if component == "api" else "index.html"
        (incoming / filename).write_text(f"new {component} {number}")
        env = dict(os.environ, TEST_BASE=str(base), TEST_FAIL=fail)
        result = subprocess.run(
            ["bash", "-s", "--", component, release_id],
            input=mocks + script, text=True, capture_output=True, env=env,
        )
        assert (result.returncode != 0) == bool(fail), result.stderr
        return (base / "current").resolve()

    api_release = deploy("api", 1)
    assert (api_release / "crocodile").read_text() == "new api 1"
    assert (api_release / "web" / "index.html").read_text() == "old web"
    # A killed previous activation can leave this symlink behind.
    (base / "current.next").symlink_to(initial)
    web_release = deploy("web", 2)
    assert (web_release / "crocodile").read_text() == "new api 1"
    assert (web_release / "web" / "index.html").read_text() == "new web 2"
    assert (base / "restarts").read_text().splitlines() == ["restart"]
    assert deploy("api", 3, "api") == web_release
    assert deploy("web", 4, "web") == web_release
    assert (initial / "crocodile").read_text() == "old api"
    invalid = subprocess.run(
        ["bash", "-s", "--", "api", "../../invalid"],
        input=mocks + script, text=True, capture_output=True,
    )
    assert invalid.returncode != 0
    assert (base / "current").resolve() == web_release
    print("PASS: isolated API/web deployment, preserved component, rollback, invalid ID")
