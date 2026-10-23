import argparse
import json
import pathlib

import numpy

from prepare import fingerprint, read_records
from train import labeled_examples, metrics, select_threshold


def paired_interval(expected, candidate, baseline, groups):
    generator = numpy.random.default_rng(42)
    unique_groups = sorted(set(groups))
    group_indexes = {group: numpy.flatnonzero(groups == group) for group in unique_groups}
    differences = []
    for iteration in range(5000):
        indexes = numpy.concatenate([group_indexes[group] for group in generator.choice(unique_groups, len(unique_groups), replace=True)])
        differences.append(numpy.mean(candidate[indexes] == expected[indexes]) - numpy.mean(baseline[indexes] == expected[indexes]))
    return numpy.quantile(differences, [0.025, 0.975]).tolist()


def evaluate(directory):
    examples = labeled_examples(directory)
    expected = numpy.array([example["label"] for example in examples])
    training = numpy.array([example["split"] == "train" for example in examples])
    validation = numpy.array([example["split"] == "validation" for example in examples])
    testing = numpy.array([example["split"] == "test" for example in examples])
    groups = numpy.array([example["group"] for example in examples])
    if any(set(groups[first]) & set(groups[second]) for first, second in [(training, validation), (training, testing), (validation, testing)]):
        raise ValueError("Conversation leakage between splits")
    baseline_records = {record["identifier"]: record for record in read_records(directory / "baseline.jsonl")}
    baseline = numpy.array([int(baseline_records[example["identifier"]]["worthReview"]) for example in examples if example["split"] == "test"])
    results = {"current_gate": {"test": metrics(expected[testing], baseline)}}
    predictions = {"current_gate": baseline}
    majority = int(expected[training].mean() >= 0.5)
    results["majority"] = {"test": metrics(expected[testing], numpy.full(testing.sum(), majority)), "label": majority}
    local_candidates = []
    for path in sorted(directory.glob("*/classifier.json")):
        information = json.loads(path.read_text())
        probabilities = numpy.load(path.parent / "probabilities.npy")
        name = path.parent.name
        prediction = probabilities[testing] >= information["threshold"]
        results[name] = dict(information, test=metrics(expected[testing], prediction))
        predictions[name] = prediction
        local_candidates.append(name)
        router_path = path.parent / "router.json"
        if router_path.exists():
            information = json.loads(router_path.read_text())
            probabilities = numpy.load(path.parent / "router_probabilities.npy")
            name += "-router"
            prediction = probabilities[testing] >= information["threshold"]
            results[name] = dict(information, test=metrics(expected[testing], prediction))
            predictions[name] = prediction
            local_candidates.append(name)
    for path in sorted(directory.glob("nli*/probabilities.npy")):
        information = json.loads((path.parent / "encoder.json").read_text())
        if information["examples_sha256"] != fingerprint((directory / "examples.jsonl").read_text()):
            raise ValueError("NLI scores belong to a different dataset")
        probabilities = numpy.load(path)
        threshold, validation_result = select_threshold(expected[validation], probabilities[validation], numpy.linspace(0.05, 0.95, 37))
        for name, cutoff in [(path.parent.name + "-zero-shot", 0.5), (path.parent.name + "-calibrated", threshold)]:
            prediction = probabilities[testing] >= cutoff
            results[name] = {"threshold": cutoff, "validation": metrics(expected[validation], probabilities[validation] >= cutoff), "test": metrics(expected[testing], prediction)}
            predictions[name] = prediction
            local_candidates.append(name)
    selected = max(local_candidates, key=lambda name: (results[name]["validation"]["accuracy"], results[name]["validation"]["macro_f1"]))
    for name, prediction in predictions.items():
        if name != "current_gate":
            results[name]["accuracy_difference_interval"] = paired_interval(expected[testing], prediction, baseline, groups[testing])
    summary = {"selected_by_validation": selected, "splits": {name: {"examples": int(mask.sum()), "positive": int(expected[mask].sum()), "groups": len(set(groups[mask]))}
               for name, mask in [("train", training), ("validation", validation), ("test", testing)]}, "results": results}
    test_examples = [example for example in examples if example["split"] == "test"]
    audit = {record["identifier"]: record for record in read_records(directory / "audit.jsonl")}
    audited_indexes = [index for index, example in enumerate(test_examples) if example["identifier"] in audit]
    audited_labels = numpy.array([int(audit[test_examples[index]["identifier"]]["worthReview"]) for index in audited_indexes])
    if audited_indexes:
        summary["label_audit"] = {"examples": len(audited_indexes), "agreement": float(numpy.mean(expected[testing][audited_indexes] == audited_labels)),
                                  "selected_accuracy": float(numpy.mean(predictions[selected][audited_indexes] == audited_labels)),
                                  "current_gate_accuracy": float(numpy.mean(baseline[audited_indexes] == audited_labels))}
    if "encoder" in results[selected]:
        from predict import score_texts
        model_directory = directory / results[selected]["encoder"]
        router = selected.endswith("-router")
        restored_scores, threshold = score_texts(model_directory, [example["text"] for example in test_examples], router)
        restored_predictions = restored_scores >= threshold
        summary["artifact_check"] = {"examples": len(test_examples), "changed_predictions_after_reload": int(numpy.sum(restored_predictions != predictions[selected]))}
        logged_examples = read_records(directory / "logged" / "examples.jsonl")
        logged_labels = {record["identifier"]: record for record in read_records(directory / "logged" / "labels.jsonl")}
        if logged_examples and all(example["identifier"] in logged_labels for example in logged_examples):
            logged_baseline = {record["identifier"]: record for record in read_records(directory / "logged" / "baseline.jsonl")}
            logged_expected = numpy.array([int(logged_labels[example["identifier"]]["worthReview"]) for example in logged_examples])
            logged_predictions = numpy.array([int(logged_baseline[example["identifier"]]["worthReview"]) for example in logged_examples])
            logged_scores, threshold = score_texts(model_directory, [example["text"] for example in logged_examples], router)
            summary["logged_sanity_check"] = {"examples": len(logged_examples), "current_gate": metrics(logged_expected, logged_predictions),
                                               "selected": metrics(logged_expected, logged_scores >= threshold)}
        control = {record["identifier"]: record for record in read_records(directory / "baseline-folder.jsonl")}
        if len(control) == len(test_examples):
            control_baseline = numpy.array([int(control[example["identifier"]]["worthReview"]) for example in test_examples])
            control_texts = [example["text"].replace("folder: (historical assignment unavailable)", "folder: Work") for example in test_examples]
            control_scores, threshold = score_texts(model_directory, control_texts, router)
            summary["folder_control"] = {"current_gate": metrics(expected[testing], control_baseline),
                                         "selected": metrics(expected[testing], control_scores >= threshold),
                                         "accuracy_difference_interval": paired_interval(expected[testing], control_scores >= threshold, control_baseline, groups[testing])}
    (directory / "results.json").write_text(json.dumps(summary, indent=2))
    errors = []
    for index, example in enumerate(test_examples):
        if predictions[selected][index] != expected[testing][index] or baseline[index] != expected[testing][index]:
            errors.append(dict(example, candidate=int(predictions[selected][index]), baseline=int(baseline[index])))
    (directory / "errors.jsonl").write_text("".join(json.dumps(record) + "\n" for record in errors))
    print(json.dumps(summary, indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--data", type=pathlib.Path, required=True)
    evaluate(parser.parse_args().data)
