import collections
import hashlib
import json
import pathlib
import sys

sys.dont_write_bytecode = True
sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1] / "gate"))
from prepare import read_records, read_session


def order(value):
    return hashlib.sha256(("den-cross-encoder-v1:" + value).encode()).hexdigest()


def write_records(path, records):
    path.write_text("".join(json.dumps(record, ensure_ascii=False) + "\n" for record in records))


def prepare():
    destination = pathlib.Path(".local/instruction-cross-encoder")
    if destination.exists():
        raise SystemExit("Sample already frozen; use the saved manifest.")
    previous = pathlib.Path(".local/instruction-evaluation")
    development = read_records(previous / "examples.jsonl")
    development_groups = {example["group"] for example in development}
    candidates = read_records(pathlib.Path(".local/gate-experiment/data/examples.jsonl"))
    groups = collections.defaultdict(list)
    sessions = {}
    unavailable = []
    for example in candidates:
        if example["group"] not in development_groups:
            source = example["source"]
            if source not in sessions:
                sessions[source] = read_session(pathlib.Path(source), example["tool"])
            session = sessions[source]
            if not session or len(session["exchanges"]) < example["exchange"]:
                unavailable.append(example["identifier"])
                continue
            groups[example["group"]].append(example)
    held_out = []
    held_out_groups = set()
    for group in sorted(groups, key=order):
        held_out_groups.add(group)
        held_out.extend(sorted(groups[group], key=lambda example: order(example["identifier"]))[:4])
        if len(held_out) >= 160:
            break
    held_out = held_out[:160]
    training = []
    for group in sorted(set(groups) - held_out_groups, key=order):
        training.extend(sorted(groups[group], key=lambda example: order(example["identifier"]))[:6])
    training = sorted(training, key=lambda example: order(example["identifier"]))[:480]
    for split, examples in (("train", training), ("validation", development), ("test", held_out)):
        for example in examples:
            example["split"] = split
            if "prompt" in example:
                continue
            source = example["source"]
            if source not in sessions:
                sessions[source] = read_session(pathlib.Path(source), example["tool"])
            session = sessions[source]
            if not session or len(session["exchanges"]) < example["exchange"]:
                raise ValueError("Cannot recover full source: " + example["identifier"])
            position = example["exchange"] - 1
            exchange = session["exchanges"][position]
            example.update(prompt=exchange["prompt"], reply=exchange["reply"],
                           previous=session["exchanges"][position - 1] if position else None,
                           full_source=True)
    destination.mkdir()
    for split, examples in (("train", training), ("validation", development), ("test", held_out)):
        directory = destination / split
        directory.mkdir()
        write_records(directory / "examples.jsonl", examples)
        (directory / "instructions.json").write_bytes((previous / "instructions.json").read_bytes())
    (destination / "validation/labels.jsonl").write_bytes((previous / "labels.jsonl").read_bytes())
    (destination / "validation/label-corrections.json").write_bytes((previous / "label-corrections.json").read_bytes())
    manifest = {
        "selection": "SHA256 seed den-cross-encoder-v1; connected groups disjoint; previous instruction evaluation reused only for validation. Fresh test selected from remaining groups before labeling/scoring; max four per test group, six per train group.",
        "criterion": "Maximize validation reviews avoided with >=95% union recall and >=95% recall for every instruction enabled alone. Tie: smaller model, then higher threshold. Freeze before test scoring/labels are inspected.",
        "limitations": "Fresh for this instruction experiment, drawn from the older corpus. Historical final replies, not intermediate commentary. Model-generated labels, not human ground truth.",
        "unavailable_sources": unavailable,
        "splits": {split: {"examples": len(examples), "groups": len({example['group'] for example in examples}),
                           "sha256": hashlib.sha256((destination / split / 'examples.jsonl').read_bytes()).hexdigest()}
                   for split, examples in (("train", training), ("validation", development), ("test", held_out))}}
    (destination / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    print(json.dumps(manifest, indent=2))


if __name__ == "__main__":
    prepare()
