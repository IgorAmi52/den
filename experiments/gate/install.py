import argparse
import hashlib
import json
import os
import pathlib
import platform
import shutil
import subprocess
import tempfile


def install(source, destination):
    model_directory = destination / "model"
    if model_directory.exists():
        raise SystemExit(f"Already installed: {model_directory}. Choose a new --destination to stage an update.")
    if not source.is_dir():
        raise SystemExit(f"Model source not found: {source}. The trained weights are distributed separately; a Git clone does not include them. DEN can run without the local classifier.")
    if platform.system() != "Darwin" or platform.machine() != "arm64":
        raise SystemExit("This pinned classifier runtime requires native Apple silicon macOS. DEN can use direct model reviews without it.")
    if shutil.which("uv") is None:
        raise SystemExit("uv not found. Install it with: brew install uv")
    inventory = json.loads(pathlib.Path(__file__).with_name("artifacts.json").read_text())
    hashes = {}
    for record in inventory["files"]:
        if not record["path"].startswith("modernbert-setfit/"):
            continue
        relative_path = record["path"].removeprefix("modernbert-setfit/")
        if not relative_path.startswith("model/") and relative_path not in ("encoder.json", "classifier.json", "classifier.joblib"):
            continue
        path = source / relative_path
        if not path.is_file():
            raise SystemExit(f"Incomplete model source: missing {path}")
        digest = hashlib.sha256()
        with path.open("rb") as artifact:
            for chunk in iter(lambda: artifact.read(1024 * 1024), b""):
                digest.update(chunk)
        if path.stat().st_size != record["bytes"] or digest.hexdigest() != record["sha256"]:
            raise SystemExit(f"Model artifact does not match the selected release: {path}")
        hashes[relative_path] = record["sha256"]
    destination.mkdir(parents=True, exist_ok=True, mode=0o700)
    if not (destination / "venv/bin/python").exists():
        subprocess.run(["uv", "venv", "--python", "3.12", str(destination / "venv")], check=True)
    subprocess.run(["uv", "pip", "install", "--python", str(destination / "venv/bin/python"),
                    "-r", str(pathlib.Path(__file__).with_name("requirements.lock.txt"))], check=True)
    version = hashlib.sha256(json.dumps(hashes, sort_keys=True).encode()).hexdigest()
    with tempfile.TemporaryDirectory(prefix=".model-staging-", dir=destination) as temporary_directory:
        staging_directory = pathlib.Path(temporary_directory) / "model"
        for relative_path in hashes:
            target = staging_directory / relative_path
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source / relative_path, target)
        (staging_directory / "manifest.json").write_text(json.dumps({"version": version, "files": hashes}, indent=2) + "\n")
        prediction = subprocess.run(
            [str(destination / "venv/bin/python"), str(pathlib.Path(__file__).resolve().parents[2] / "internal/brain/classifier.py"), str(staging_directory)],
            input=json.dumps({"texts": ["SESSION\nInstallation verification: no new findings."]}),
            text=True, capture_output=True, timeout=90, check=True,
        )
        result = json.loads(prediction.stdout)
        if result["modelVersion"] != version or result["threshold"] != 0.625 or len(result["scores"]) != 1 or not 0 <= result["scores"][0] <= 1:
            raise SystemExit("The installed classifier did not pass its prediction check.")
        staging_directory.rename(model_directory)
    print(json.dumps({"destination": str(destination), "version": version}, indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", type=pathlib.Path, required=True)
    parser.add_argument("--destination", type=pathlib.Path,
                        default=pathlib.Path(os.environ.get("DEN_HOME", str(pathlib.Path.home() / ".den"))) / "gate")
    arguments = parser.parse_args()
    try:
        install(arguments.source.resolve(), arguments.destination.resolve())
    except subprocess.CalledProcessError as failure:
        raise SystemExit(f"Classifier installation failed: {failure.stderr or failure}. No new model was activated; fix the error and retry.")
    except subprocess.TimeoutExpired:
        raise SystemExit("Classifier verification exceeded 90 seconds. No model was activated; retry or use direct reviews.")
    except (OSError, ValueError, KeyError) as failure:
        raise SystemExit(f"Classifier installation failed: {failure}")
