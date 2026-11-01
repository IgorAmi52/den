import argparse
import importlib.util
import json
import pathlib
import time
import sys

sys.dont_write_bytecode = True

import numpy

repository = pathlib.Path(__file__).resolve().parents[2]
specification = importlib.util.spec_from_file_location("instruction_encoder", repository / "internal/brain/classifier.py")
module = importlib.util.module_from_spec(specification)
specification.loader.exec_module(module)

MODELS = {
    "bge-small": (repository / ".local/gate-experiment/huggingface/hub/models--BAAI--bge-small-en-v1.5/snapshots/5c38ec7c405ec4b44b94cc5a9bb96e735b38267a", ""),
    "modernbert-setfit": (repository / ".local/gate-experiment/data/modernbert-setfit/model", "classification: "),
    "qwen": (repository / ".local/gate-experiment/huggingface/hub/models--Qwen--Qwen3-Embedding-0.6B/snapshots", ""),
}

MODELS["bge-small-behavior"] = MODELS["bge-small"]


def records(path):
    return [json.loads(line) for line in path.read_text().splitlines() if line.strip()]


def score(directory, model_name):
    examples = records(directory / "examples.jsonl")
    instructions = json.loads((directory / "instructions.json").read_text())
    if model_name.endswith("-behavior"):
        instructions = [dict(instruction, examples=instruction["examples"] + [instruction["text"]]) for instruction in instructions]
    model_path, prefix = MODELS[model_name]
    if model_name == "qwen":
        model_path = next(model_path.iterdir())
    started = time.monotonic()
    encoder = module.InstructionEncoder(model_path, prefix)
    startup_seconds = time.monotonic() - started
    texts = ["User: " + example["prompt"].strip() + "\nAssistant: " + example["reply"].strip() for example in examples]
    for position, example in enumerate(examples):
        if example["previous"] and not model_name.endswith("-behavior"):
            previous = (example["previous"]["prompt"] + "\n" + example["previous"]["reply"]).strip()
            if len(previous) > 1600:
                previous = "…" + previous[-1599:]
            texts[position] = "Previous context: " + previous + "\nNew exchange\n" + texts[position]
    started = time.monotonic()
    scores = encoder.score(texts, instructions)
    seconds = time.monotonic() - started
    timings = []
    for text in texts[:12]:
        started = time.monotonic()
        encoder.score([text], instructions)
        timings.append(time.monotonic() - started)
    result = {"model": model_name, "scores": scores, "identifiers": [example["identifier"] for example in examples],
              "startup_seconds": startup_seconds, "scoring_seconds": seconds,
              "warm_median_seconds": float(numpy.median(timings)), "warm_p95_seconds": float(numpy.quantile(timings, .95)),
              "device": encoder.device}
    (directory / (model_name + ".json")).write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({key: value for key, value in result.items() if key not in ("scores", "identifiers")}, indent=2))


def measure(examples, labels, scores, threshold, split):
    positions = [position for position, example in enumerate(examples) if example["split"] == split]
    reviews = 0
    relevant = 0
    misses = 0
    per_instruction = {}
    for identifier in scores:
        actual = [labels[examples[position]["identifier"]][identifier] for position in positions]
        passed = [max(values[position] for values in scores.values()) >= threshold for position in positions]
        own_match = [scores[identifier][position] >= threshold for position in positions]
        count = sum(actual)
        per_instruction[identifier] = {"positives": count, "misses_after_union_gate": sum(value and not predicted for value, predicted in zip(actual, passed)),
                                       "own_example_recall": sum(value and predicted for value, predicted in zip(actual, own_match)) / count if count else None,
                                       "isolated_reviews": sum(own_match), "isolated_savings": 1 - sum(own_match)/len(positions)}
    batches = {}
    for position in positions:
        example = examples[position]
        actual = any(labels[example["identifier"]].values())
        predicted = max(values[position] for values in scores.values()) >= threshold
        reviews += predicted
        relevant += actual
        misses += actual and not predicted
        batch = (example["session"], (example["exchange"] - 1) // 3)
        batches[batch] = batches.get(batch, False) or predicted
    return {"exchanges": len(positions), "relevant": relevant, "misses": misses,
            "recall": (relevant - misses) / relevant if relevant else None,
            "reviews": reviews, "reviews_avoided": len(positions) - reviews,
            "savings": 1 - reviews / len(positions), "per_instruction": per_instruction,
            "sampled_three_exchange_batches": len(batches), "batch_reviews": sum(batches.values())}


def report(directory, reveal_test):
    examples = records(directory / "examples.jsonl")
    labels = {record["identifier"]: {item["identifier"]: item["review"] for item in record["instructions"]} for record in records(directory / "labels.jsonl")}
    corrections_path = directory / "label-corrections.json"
    if corrections_path.exists():
        for correction in json.loads(corrections_path.read_text()):
            labels[correction["identifier"]][correction["instruction"]] = correction["review"]
    if len(labels) != len(examples):
        raise SystemExit("Wait for all labels before selecting thresholds")
    comparison = []
    for name in MODELS:
        path = directory / (name + ".json")
        if not path.exists():
            continue
        scores = json.loads(path.read_text())["scores"]
        candidates = []
        for threshold in numpy.arange(.05, .901, .005):
            metrics = measure(examples, labels, scores, float(threshold), "validation")
            if metrics["recall"] >= .95 and all(item["positives"] == 0 or item["own_example_recall"] >= .95 for item in metrics["per_instruction"].values()):
                candidates.append((float(threshold), metrics))
        threshold, metrics = max(candidates, key=lambda item: (item[1]["savings"], item[0]))
        comparison.append({"model": name, "threshold": threshold, "validation": metrics})
    selection = directory / "selection.json"
    if not reveal_test:
        if selection.exists():
            raise SystemExit("Selection already frozen. Do not retune on test results.")
        best = max(comparison, key=lambda item: item["validation"]["savings"])
        selection.write_text(json.dumps({"selected": best, "comparison": comparison,
                                        "criterion": "Maximize validation reviews avoided with at least 95% recall overall AND when each instruction is enabled alone. Frozen before revealing test. Ties prefer the smaller encoder by declaration order."}, indent=2) + "\n")
        print(selection.read_text())
        return
    frozen = json.loads(selection.read_text())
    best = frozen["selected"]
    scores = json.loads((directory / (best["model"] + ".json")).read_text())["scores"]
    frozen["test"] = measure(examples, labels, scores, best["threshold"], "test")
    frozen["missed_identifiers"] = [example["identifier"] for position, example in enumerate(examples)
                                  if example["split"] == "test" and any(labels[example["identifier"]].values())
                                  and max(values[position] for values in scores.values()) < best["threshold"]]
    (directory / "results.json").write_text(json.dumps(frozen, indent=2) + "\n")
    print(json.dumps(frozen, indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=["score", "select", "test"])
    parser.add_argument("--model", choices=MODELS, default="bge-small")
    parser.add_argument("--data", type=pathlib.Path, default=pathlib.Path(".local/instruction-evaluation"))
    arguments = parser.parse_args()
    if arguments.action == "score":
        score(arguments.data, arguments.model)
    else:
        report(arguments.data, arguments.action == "test")
