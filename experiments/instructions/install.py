import argparse
import hashlib
import json
import os
import pathlib
import shutil
import subprocess
import tempfile


def install(arguments):
    release_name = "trained_release.json" if arguments.model == "trained-minilm" else "release.json"
    release = json.loads(pathlib.Path(__file__).with_name(release_name).read_text())
    source = arguments.source
    if source is None and arguments.model == "trained-minilm":
        source = pathlib.Path(__file__).resolve().parents[2] / ".local/instruction-cross-encoder/models/trained-new-epoch-3"
    if source is not None and not source.is_dir():
        raise SystemExit("Trained checkpoint unavailable. Supply --source with the evaluated trained-new-epoch-3 checkpoint.")
    if arguments.model == "trained-minilm":
        for name, expected_hash in release["files"].items():
            artifact = source / name
            if not artifact.is_file() or hashlib.sha256(artifact.read_bytes()).hexdigest() != expected_hash:
                raise SystemExit(f"The trained checkpoint does not match its evaluated release: {name}")
    destination = arguments.destination.resolve()
    if destination.exists():
        raise SystemExit(f"Already exists: {destination}. Stage a new installation separately before replacing an existing runtime.")
    destination.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".instruction-gate-", dir=destination.parent) as temporary:
        staging = pathlib.Path(temporary) / "runtime"
        staging.mkdir()
        if arguments.runtime:
            (staging / "venv").symlink_to(arguments.runtime.resolve(), target_is_directory=True)
        else:
            subprocess.run(["uv", "venv", "--python", "3.12", str(staging / "venv")], check=True)
            subprocess.run(["uv", "pip", "install", "--python", str(staging / "venv/bin/python"),
                            "sentence-transformers==5.7.0", "torch==2.14.1", "transformers==4.57.6", "numpy==2.5.4"], check=True)
        python = str(staging / "venv/bin/python")
        if source:
            shutil.copytree(source.resolve(), staging / "model", symlinks=False)
        else:
            subprocess.run([python, "-c", "from huggingface_hub import snapshot_download; import sys; snapshot_download(sys.argv[1], revision=sys.argv[2], local_dir=sys.argv[3])",
                            release["model"], release["revision"], str(staging / "model")], check=True)
        files = {}
        for path in sorted((staging / "model").rglob("*")):
            if path.is_file():
                with path.open("rb") as artifact:
                    digest = hashlib.sha256()
                    for chunk in iter(lambda: artifact.read(1024 * 1024), b""):
                        digest.update(chunk)
                files[str(path.relative_to(staging))] = digest.hexdigest()
        manifest = {"kind": release.get("kind", "cosine"), "model": release["model"], "revision": release["revision"], "prefix": release["prefix"],
                    "threshold": release["threshold"], "files": files}
        (staging / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
        program = pathlib.Path(__file__).resolve().parents[2] / "internal/brain/classifier.py"
        request = {"texts": ["User: The sync job loses updates.\nAssistant: I confirmed the lost updates."],
                   "instructions": [{"identifier": "check", "text": "Catch issues mentioned in my sessions and keep their status current.",
                                     "examples": ["The sync job loses updates."]}]}
        completed = subprocess.run([python, str(program), str(staging)], input=json.dumps(request) + "\n", text=True,
                                   capture_output=True, check=True, timeout=90)
        answer = json.loads(completed.stdout)
        if len(answer["scores"]) != 1 or not 0 <= answer["scores"][0] <= 1 or answer["scores"][0] < release["threshold"]:
            raise SystemExit("Instruction runtime failed its positive-case check; installation not activated.")
        staging.rename(destination)
    print(json.dumps({"destination": str(destination), "model": release["model"], "threshold": release["threshold"],
                      "status": "local gate installed; see CROSS_ENCODER_RESULTS.md for measured limitations"}, indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Install the local instruction gate. Direct review remains the fallback.")
    parser.add_argument("--model", choices=["trained-minilm", "cosine"], default="trained-minilm")
    parser.add_argument("--source", type=pathlib.Path, help="evaluated checkpoint directory; cosine mode can download its pinned public release")
    parser.add_argument("--runtime", type=pathlib.Path, help="reuse an existing compatible Python venv; otherwise create one with uv")
    parser.add_argument("--destination", type=pathlib.Path,
                        default=pathlib.Path(os.environ.get("DEN_HOME", str(pathlib.Path.home() / ".den"))) / "instruction-gate")
    install(parser.parse_args())
