import argparse
import json
import pathlib
import sys

sys.dont_write_bytecode = True

import numpy

from cross_encoder import DIRECTORY, PairEncoder, labels, measure, records, save


def interval(examples, values):
    groups = sorted({example["group"] for example in examples})
    positions = [[position for position, example in enumerate(examples) if example["group"] == group] for group in groups]
    generator = numpy.random.default_rng(42)
    samples = []
    for _ in range(2000):
        selected = numpy.concatenate([positions[position] for position in generator.integers(0, len(groups), len(groups))])
        sample = numpy.array(values, dtype=float)[selected]
        if not numpy.isnan(sample).all():
            samples.append(float(numpy.nanmean(sample)))
    return numpy.quantile(samples, [.025, .975]).tolist()


def historical():
    selection = json.loads((DIRECTORY / "selection.json").read_text())
    directory = DIRECTORY / "test"
    examples = records(directory / "examples.jsonl")
    actual = labels(DIRECTORY / "test-clarified")
    original_labels = labels(directory)
    actual_union = numpy.array([any(actual[example["identifier"]].values()) for example in examples])
    results = []
    predictions = {}
    for candidate in selection["comparison"]:
        path = directory / (candidate["name"] + ".json")
        if not path.exists():
            continue
        artifact = json.loads(path.read_text())
        if artifact["identifiers"] != [example["identifier"] for example in examples]:
            raise ValueError("Score/example ordering mismatch")
        predicted = numpy.max(list(artifact["scores"].values()), axis=0) >= candidate["threshold"]
        predictions[candidate["name"]] = predicted
        metrics = measure(examples, actual, artifact["scores"], candidate["threshold"])
        metrics["original_label_sensitivity"] = measure(examples, original_labels, artifact["scores"], candidate["threshold"])
        metrics["savings_group_bootstrap_95"] = interval(examples, (~predicted).astype(float))
        metrics["positive_groups"] = len({example["group"] for position, example in enumerate(examples) if actual_union[position]})
        metrics["groups_with_a_missed_review"] = len({example["group"] for position, example in enumerate(examples)
                                                    if actual_union[position] and not predicted[position]})
        errors = []
        for position, example in enumerate(examples):
            missed = [identifier for identifier, values in artifact["scores"].items()
                      if actual[example["identifier"]][identifier] and values[position] < candidate["threshold"]]
            if missed or predicted[position] != actual_union[position]:
                errors.append({"identifier": example["identifier"], "missed_instructions": missed,
                               "union_miss": bool(actual_union[position] and not predicted[position]),
                               "unnecessary_review": bool(predicted[position] and not actual_union[position])})
        results.append({"name": candidate["name"], "threshold": candidate["threshold"], "metrics": metrics,
                        "errors": errors, "selected": candidate["name"] == selection["selected"]["name"]})
    comparisons = {}
    selected_predictions = predictions[selection["selected"]["name"]]
    for name, predicted in predictions.items():
        comparisons[name] = {"additional_reviews_avoided": int(predicted.sum() - selected_predictions.sum()),
                            "savings_difference_group_bootstrap_95": interval(examples, predicted.astype(float) - selected_predictions.astype(float))}
    save(DIRECTORY / "results.json", {"selected": selection["selected"]["name"], "historical": results, "paired_comparisons": comparisons,
         "primary_labels": "test-clarified: chosen before inspecting test labels or predictions; original labels retained as sensitivity analysis"})
    print(json.dumps([{key: value for key, value in result.items() if key != "errors"} for result in results], indent=2))


def diagnostic_result(selected):
    if selected["model"] == "cosine":
        raise SystemExit("No cross-encoder selected; score cross-encoder diagnostics separately.")
    encoder = PairEncoder(selected["model"], selected["context"])
    specifications = json.loads(pathlib.Path("experiments/instructions/cross_encoder_challenges.json").read_text())
    known = json.loads(pathlib.Path("experiments/instructions/instructions.json").read_text())
    instructions = {instruction["identifier"]: instruction for instruction in known + specifications["instructions"]}
    results = []
    for case in specifications["cases"]:
        instruction = instructions[case["instruction"]]
        score = encoder.score([case], [instruction])[instruction["identifier"]][0]
        predicted = score >= selected["threshold"]
        results.append({"identifier": case["identifier"], "instruction": case["instruction"], "score": score,
                        "expected": case["review"], "predicted": predicted, "passed": predicted == case["review"],
                        "unseen_instruction": instruction in specifications["instructions"]})
    scenarios = json.loads(pathlib.Path("experiments/instructions/scenarios.json").read_text())
    reviewer = {result["name"]: result for result in json.loads(pathlib.Path(".local/instruction-replay/results.json").read_text())}
    pipeline = []
    for scenario in scenarios:
        score = encoder.score([scenario], [instructions[scenario["instruction"]]])[scenario["instruction"]][0]
        predicted = score >= selected["threshold"]
        old_result = reviewer[scenario["name"]]
        passed = old_result["passed"] if predicted else not scenario["change"]
        pipeline.append({"name": scenario["name"], "score": score, "review": predicted,
                         "expected_change": scenario["change"], "passed": passed,
                         "reviewer_source": "previous cached Sol result; no new provider call"})
    for name, subset in (("known", [result for result in results if not result["unseen_instruction"]]),
                         ("unseen", [result for result in results if result["unseen_instruction"]]),
                         ("stale_context", [result for result in results if result["identifier"].endswith("-stale")]),
                         ("pipeline", pipeline)):
        print(selected["name"], name, sum(result["passed"] for result in subset), "/", len(subset))
    return {"challenges": results, "pipeline_replay": pipeline}


def diagnostics():
    selection = json.loads((DIRECTORY / "selection.json").read_text())
    names = {selection["selected"]["name"], "trained-context-epoch-3", "trained-new-epoch-3"}
    results = {candidate["name"]: diagnostic_result(candidate) for candidate in selection["comparison"]
               if candidate["name"] in names}
    save(DIRECTORY / "diagnostics.json", {"selected": selection["selected"]["name"], "models": results})


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=["historical", "diagnostics"])
    arguments = parser.parse_args()
    {"historical": historical, "diagnostics": diagnostics}[arguments.action]()
