import argparse
import hashlib
import json
import pathlib


def verify(root):
    manifest = json.loads(pathlib.Path(__file__).with_name("artifacts.json").read_text())
    checked = 0
    for directory, records in [(manifest["directory"], manifest["files"]),
                               (manifest["pretrainedCacheDirectory"], manifest["pretrainedFiles"])]:
        for record in records:
            path = root / directory / record["path"]
            digest = hashlib.sha256()
            with path.open("rb") as artifact:
                for chunk in iter(lambda: artifact.read(1024 * 1024), b""):
                    digest.update(chunk)
            if path.stat().st_size != record["bytes"] or digest.hexdigest() != record["sha256"]:
                raise ValueError(f"Artifact changed: {path}")
            checked += 1
    print(f"Verified {checked} archived files.")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=pathlib.Path, default=pathlib.Path(__file__).resolve().parents[2])
    verify(parser.parse_args().root)
