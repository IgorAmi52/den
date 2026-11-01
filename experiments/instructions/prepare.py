import argparse
import hashlib
import json
import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1] / "gate"))
from prepare import read_records, read_session


def prepare(source, destination):
    destination.mkdir(parents=True, exist_ok=True)
    target = destination / "examples.jsonl"
    if target.exists():
        raise SystemExit("Evaluation sample is already frozen. Use a new destination for a new experiment.")
    original = read_records(source / "examples.jsonl")
    selected = []
    for split in ("validation", "test"):
        candidates = sorted((example for example in original if example["split"] == split),
                            key=lambda example: hashlib.sha256(("den-instructions-v1:" + example["identifier"]).encode()).hexdigest())
        per_session = {}
        for example in candidates:
            if per_session.get(example["session"], 0) >= 4:
                continue
            selected.append(example)
            per_session[example["session"]] = per_session.get(example["session"], 0) + 1
            if sum(per_session.values()) == 96:
                break
    sessions = {}
    results = []
    for example in selected:
        source_path = pathlib.Path(example["source"])
        if example["source"] not in sessions:
            sessions[example["source"]] = read_session(source_path, example["tool"]) if source_path.exists() else None
        session = sessions[example["source"]]
        record = dict(example)
        record["full_source"] = bool(session and len(session["exchanges"]) >= example["exchange"])
        if record["full_source"]:
            position = example["exchange"] - 1
            exchange = session["exchanges"][position]
            record["prompt"], record["reply"] = exchange["prompt"], exchange["reply"]
            record["previous"] = session["exchanges"][position - 1] if position else None
        else:
            text = example["text"].split("\nuser: ", 1)[1]
            record["prompt"], record["reply"] = text.split("\nagent: ", 1)
            record["previous"] = None
        results.append(record)
    target.write_text("".join(json.dumps(record, ensure_ascii=False) + "\n" for record in results))
    instructions = pathlib.Path(__file__).with_name("instructions.json").read_bytes()
    (destination / "instructions.json").write_bytes(instructions)
    summary = {"sample_sha256": hashlib.sha256(target.read_bytes()).hexdigest(),
               "instructions_sha256": hashlib.sha256(instructions).hexdigest(),
               "examples": len(results), "full_source": sum(record["full_source"] for record in results),
               "groups": len({record["group"] for record in results}),
               "split": "Original grouped validation/test splits; no original training examples; max four per session.",
               "selection": "SHA256 order using den-instructions-v1 seed, 96 per split, frozen before labeling or model scoring."}
    (destination / "manifest.json").write_text(json.dumps(summary, indent=2) + "\n")
    print(json.dumps(summary, indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", type=pathlib.Path, default=pathlib.Path(".local/gate-experiment/data"))
    parser.add_argument("--destination", type=pathlib.Path, default=pathlib.Path(".local/instruction-evaluation"))
    arguments = parser.parse_args()
    prepare(arguments.source, arguments.destination)
