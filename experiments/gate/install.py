import argparse
import hashlib
import json
import os
import pathlib
import shutil
import subprocess


def install(source, destination):
    encoder = json.loads((source / "encoder.json").read_text())
    classifier = json.loads((source / "classifier.json").read_text())
    if encoder["model"] != "nomic-ai/modernbert-embed-base" or not encoder.get("fine_tuned") or classifier["threshold"] != 0.625:
        raise SystemExit("Install the validation-selected ModernBERT SetFit model with threshold 0.625.")
    destination.mkdir(parents=True, exist_ok=True, mode=0o700)
    model_directory = destination / "model"
    if model_directory.exists():
        raise SystemExit(f"Already installed: {model_directory}. Choose a new --destination to stage an update.")
    if not (destination / "venv/bin/python").exists():
        subprocess.run(["uv", "venv", "--python", "3.12", str(destination / "venv")], check=True)
    subprocess.run(["uv", "pip", "install", "--python", str(destination / "venv/bin/python"),
                    "-r", str(pathlib.Path(__file__).with_name("requirements.lock.txt"))], check=True)
    staging_directory = destination / "model-staging"
    shutil.copytree(source / "model", staging_directory / "model", dirs_exist_ok=True)
    for name in ("encoder.json", "classifier.json", "classifier.joblib"):
        shutil.copy2(source / name, staging_directory / name)
    hashes = {}
    for path in sorted(staging_directory.rglob("*")):
        if path.is_file() and path.name != "manifest.json":
            with path.open("rb") as artifact:
                digest = hashlib.sha256()
                for chunk in iter(lambda: artifact.read(1024 * 1024), b""):
                    digest.update(chunk)
                hashes[path.relative_to(staging_directory).as_posix()] = digest.hexdigest()
    version = hashlib.sha256(json.dumps(hashes, sort_keys=True).encode()).hexdigest()
    (staging_directory / "manifest.json").write_text(json.dumps({"version": version, "files": hashes}, indent=2) + "\n")
    staging_directory.rename(model_directory)
    print(json.dumps({"destination": str(destination), "version": version}, indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", type=pathlib.Path, required=True)
    parser.add_argument("--destination", type=pathlib.Path,
                        default=pathlib.Path(os.environ.get("DEN_HOME", str(pathlib.Path.home() / ".den"))) / "gate")
    arguments = parser.parse_args()
    install(arguments.source.resolve(), arguments.destination.resolve())
