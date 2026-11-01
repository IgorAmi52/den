import argparse
import collections
import hashlib
import json
import pathlib
import random
import statistics
import time

from pretrained_gate import Gate, confidence_intervals, memory, metrics, read_records, save

DIRECTORY = pathlib.Path(".local/gliclass-finetuning")
HISTORICAL = pathlib.Path(".local/instruction-cross-encoder")
DESCRIPTIONS = ["contains information requested by the rule", "does not contain information requested by the rule"]


def fingerprint(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def partition(name):
    path = DIRECTORY / (name + ".json")
    manifest = json.loads((DIRECTORY / "manifest.json").read_text())
    assert fingerprint(path) == manifest["partitions"][name]["sha256"]
    specification = json.loads(path.read_text())
    identifiers = {instruction["text"]: instruction["identifier"] for instruction in specification["instructions"]}
    known = set(identifiers.values())
    for case in specification["cases"]:
        case["reviews"] = {identifiers.get(identifier, identifier): review for identifier, review in case["reviews"].items()}
        if "instruction" in case:
            case["instruction"] = identifiers.get(case["instruction"], case["instruction"])
        assert set(case["reviews"]) <= known
    return specification


def historical_partition(name):
    directory = HISTORICAL / name
    instructions = json.loads((directory / "instructions.json").read_text())
    actual = {record["identifier"]: {item["identifier"]: item["review"] for item in record["instructions"]}
              for record in read_records(directory / "labels.jsonl")}
    corrections = directory / "label-corrections.json"
    if corrections.exists():
        for correction in json.loads(corrections.read_text()):
            actual[correction["identifier"]][correction["instruction"]] = correction["review"]
    cases = [dict(example, reviews=actual[example["identifier"]], source="historical")
             for example in read_records(directory / "examples.jsonl")]
    return {"instructions": instructions, "cases": cases}


def authored_partition(path):
    specification = json.loads(path.read_text())
    audit_path = path.with_suffix(".audit.json")
    corrections = {}
    if audit_path.exists():
        corrections = {item["identifier"]: item["review"] for item in json.loads(audit_path.read_text())["corrections"]}
    cases = [dict(case, reviews={case["instruction"]: corrections.get(case["identifier"], case["review"])}, source="authored")
             for case in specification["cases"]]
    return {"instructions": specification["instructions"], "cases": cases}


def prepare():
    if (DIRECTORY / "manifest.json").exists():
        raise SystemExit("Dataset already frozen")
    for name in ["training", "development", "heldout"]:
        source = json.loads((DIRECTORY / ("authored-" + name + ".json")).read_text())
        audit = json.loads((DIRECTORY / ("authored-" + name + ".audit.json")).read_text())
        assert audit["reviewed_count"] == len(source["cases"])
        identifiers = {case["identifier"] for case in source["cases"]}
        assert all(item["identifier"] in identifiers for item in audit["corrections"] + audit["ambiguous"])
    training = historical_partition("train")
    previous = authored_partition(pathlib.Path("experiments/instructions/pretrained_gate_challenges.json"))
    training["instructions"] = previous["instructions"]
    training["cases"].extend(previous["cases"])
    training["cases"].extend(authored_partition(DIRECTORY / "authored-training.json")["cases"])
    training_audit = json.loads((DIRECTORY / "authored-training.audit.json").read_text())
    ambiguous = {item["identifier"] for item in training_audit["ambiguous"]}
    training["cases"] = [case for case in training["cases"] if not case.get("ambiguous") and case["identifier"] not in ambiguous]
    development = historical_partition("validation")
    authored = authored_partition(DIRECTORY / "authored-development.json")
    development["instructions"].extend(authored["instructions"])
    development["cases"].extend(authored["cases"])
    heldout = authored_partition(DIRECTORY / "authored-heldout.json")
    historical = historical_partition("test-clarified")
    partitions = {"training": training, "development": development, "heldout": heldout, "historical": historical}
    groups = {}
    for name, specification in partitions.items():
        identifiers = [case["identifier"] for case in specification["cases"]]
        assert len(identifiers) == len(set(identifiers)), name
        groups[name] = {case["group"] for case in specification["cases"]}
        save(DIRECTORY / (name + ".json"), specification)
    assert not groups["training"] & groups["development"]
    assert not groups["training"] & groups["historical"]
    known = {instruction["identifier"] for specification in [training, development] for instruction in specification["instructions"]}
    assert not known & {instruction["identifier"] for instruction in heldout["instructions"]}
    for other in [training, development, historical]:
        passages = {(case["prompt"].strip(), case["reply"].strip()) for case in other["cases"]}
        assert not passages & {(case["prompt"].strip(), case["reply"].strip()) for case in heldout["cases"]}
    inputs = list(DIRECTORY.glob("authored-*.json")) + [pathlib.Path("experiments/instructions/pretrained_gate_challenges.json")]
    inputs.extend(HISTORICAL.glob("*/examples.jsonl"))
    inputs.extend(HISTORICAL.glob("*/labels.jsonl"))
    inputs.extend(HISTORICAL.glob("*/label-corrections.json"))
    save(DIRECTORY / "manifest.json", {
        "inputs": {str(path): fingerprint(path) for path in inputs},
        "partitions": {name: {"cases": len(specification["cases"]), "instructions": len(specification["instructions"]),
                              "sha256": fingerprint(DIRECTORY / (name + ".json"))} for name, specification in partitions.items()},
        "recipe": {"epochs": 2, "sampled_pairs_per_epoch": 960, "batch_size": 8, "learning_rate": 0.00001,
                   "trainable": "last four encoder layers and classification/calibration modules", "seed": 42,
                   "approved_examples": "included independently on half of training pairs",
                   "objective": "binary cross entropy on the strongest passage; hard passage selected in evaluation mode",
                   "selection": "maximize development skips at >=95% overall and every-instruction recall; compare examples on/off",
                   "operating_points": [0.9, 0.95, 0.99], "fixed_threshold_diagnostics": [0.1, 0.25, 0.5, 0.75]}})
    print(json.dumps({name: len(specification["cases"]) for name, specification in partitions.items()}))


class InstructionGate(Gate):
    def __init__(self, checkpoint="pretrained", training=False):
        super().__init__("gliclass", True, "bfloat16", 4, "simple", "mps")
        if checkpoint != "pretrained":
            from safetensors.torch import load_file

            self.model.load_state_dict(load_file(str(DIRECTORY / "models" / (checkpoint + ".safetensors"))))
        if training:
            self.model.float()
            for parameter in self.model.model.encoder_model.parameters():
                parameter.requires_grad = False
            for layer in self.model.model.encoder_model.encoder.layer[-4:]:
                for parameter in layer.parameters():
                    parameter.requires_grad = True

    def rendered(self, case, instruction, examples):
        prompt = "User monitoring rule: " + instruction["text"] + "\nClassify the new text according to this rule.\n"
        approved = []
        if examples:
            approved = [{"text": example, "labels": [DESCRIPTIONS[0]]} for example in instruction.get("examples", [])]
        passages = [self.pipeline.prepare_input(passage, DESCRIPTIONS, prompt=prompt, examples=approved)
                    for passage in self.windows(case)]
        assert all(len(self.encode(passage)) + self.tokenizer.num_special_tokens_to_add() <= 768 for passage in passages)
        return passages

    def logits(self, passages):
        inputs = self.tokenizer(passages, padding=True, truncation=False, return_tensors="pt").to(self.device)
        logits = self.model(**inputs, max_num_classes=2).logits[:, :2].float()
        return logits[:, 0] - logits[:, 1]

    def probabilities(self, passages):
        self.model.eval()
        values = []
        with self.torch.inference_mode():
            for position in range(0, len(passages), 8):
                values.extend(self.logits(passages[position:position + 8]).sigmoid().cpu().tolist())
        return values

    def training_mode(self):
        self.model.train()
        self.model.model.encoder_model.embeddings.eval()
        for layer in self.model.model.encoder_model.encoder.layer[:-4]:
            layer.eval()


def train(smoke=False):
    import torch
    from safetensors.torch import save_file

    if (DIRECTORY / "selection.json").exists() or (DIRECTORY / "models/epoch-1.safetensors").exists():
        raise SystemExit("Training already started or selection frozen")
    gate = InstructionGate(training=True)
    parameters = [parameter for parameter in gate.model.parameters() if parameter.requires_grad]
    optimizer = torch.optim.AdamW(parameters, lr=0.00001, weight_decay=0.01)
    generator = random.Random(42)
    if smoke:
        specification = historical_partition("train")
    else:
        specification = partition("training")
    instructions = {instruction["identifier"]: instruction for instruction in specification["instructions"]}
    buckets = collections.defaultdict(list)
    for case in specification["cases"]:
        for identifier, review in case["reviews"].items():
            buckets[(identifier, review)].append(case)
    assert len(buckets) == len(instructions) * 2
    keys = sorted(buckets)
    started = time.perf_counter()
    history = []
    sampled_pairs = set()
    for epoch in range(1, 3):
        losses = []
        batches = 1 if smoke else 120
        for position in range(batches):
            passages = []
            targets = []
            for _ in range(8):
                identifier, review = generator.choice(keys)
                case = generator.choice(buckets[(identifier, review)])
                sampled_pairs.add((case["identifier"], identifier))
                rendered = gate.rendered(case, instructions[identifier], generator.random() < 0.5)
                with torch.autocast("mps", dtype=torch.bfloat16):
                    values = gate.probabilities(rendered) if len(rendered) > 1 else [1.]
                passages.append(rendered[max(range(len(values)), key=values.__getitem__)])
                targets.append(float(review))
            gate.training_mode()
            optimizer.zero_grad(set_to_none=True)
            with torch.autocast("mps", dtype=torch.bfloat16):
                predictions = gate.logits(passages)
                loss = torch.nn.functional.binary_cross_entropy_with_logits(predictions, torch.tensor(targets, device="mps"))
            assert torch.isfinite(loss)
            loss.backward()
            gradient_norm = torch.nn.utils.clip_grad_norm_(parameters, 1.)
            assert torch.isfinite(gradient_norm) and gradient_norm > 0
            optimizer.step()
            losses.append(loss.item())
            if position % 10 == 0 or position + 1 == batches:
                print(json.dumps({"epoch": epoch, "batch": position + 1, "loss": statistics.mean(losses),
                                  "gradient_norm": float(gradient_norm), "seconds": time.perf_counter() - started}), flush=True)
        if smoke:
            print(json.dumps({"smoke": "forward/backward/update finite", "trainable_parameters": sum(parameter.numel() for parameter in parameters),
                              "total_parameters": gate.parameters, "memory": memory()}), flush=True)
            return
        output = DIRECTORY / "models" / ("epoch-" + str(epoch) + ".safetensors")
        output.parent.mkdir(parents=True, exist_ok=True)
        save_file({name: tensor.detach().cpu().contiguous() for name, tensor in gate.model.state_dict().items()}, str(output))
        history.append({"epoch": epoch, "loss": statistics.mean(losses), "seconds": time.perf_counter() - started,
                        "checkpoint_sha256": fingerprint(output)})
        save(DIRECTORY / "training-history.json", {"epochs": history, "manifest_sha256": fingerprint(DIRECTORY / "manifest.json"),
             "script_sha256": fingerprint(pathlib.Path(__file__)), "trainable_parameters": sum(parameter.numel() for parameter in parameters),
             "total_parameters": gate.parameters, "unique_sampled_pairs": len(sampled_pairs),
             "available_pairs": sum(len(bucket) for bucket in buckets.values()), "memory": memory()})


def score(arguments):
    name = arguments.checkpoint + ("-examples" if arguments.examples else "-plain")
    output = DIRECTORY / "scores" / arguments.split / (name + ".json")
    if output.exists():
        raise SystemExit("Completed scores already exist")
    if arguments.split in {"heldout", "historical"} and not (DIRECTORY / "selection.json").exists():
        raise SystemExit("Freeze selection first")
    specification = partition(arguments.split)
    instructions = {instruction["identifier"]: instruction for instruction in specification["instructions"]}
    started = time.perf_counter()
    gate = InstructionGate(arguments.checkpoint)
    load_seconds = time.perf_counter() - started
    results = []
    for position, case in enumerate(specification["cases"]):
        started = time.perf_counter()
        scores = {identifier: max(gate.probabilities(gate.rendered(case, instructions[identifier], arguments.examples)))
                  for identifier in case["reviews"]}
        results.append({"identifier": case["identifier"], "group": case["group"], "scores": scores,
                        "seconds": time.perf_counter() - started})
        if (position + 1) % 32 == 0:
            print(json.dumps({"name": name, "completed": position + 1, "total": len(specification["cases"])}), flush=True)
    save(output, {"name": name, "checkpoint": arguments.checkpoint, "approved_examples": arguments.examples,
                  "precision": "bfloat16", "device": "mps", "results": results, "memory": memory(), "load_seconds": load_seconds,
                  "data_sha256": fingerprint(DIRECTORY / (arguments.split + ".json")), "script_sha256": fingerprint(pathlib.Path(__file__))})


def operating_point(results, actual, target):
    values = sorted({value for result in results for value in result["scores"].values()})
    thresholds = [0.] + [(first + second) / 2 for first, second in zip(values, values[1:])]
    selected = None
    for threshold in thresholds:
        measured = metrics(results, actual, threshold)
        if measured["recall"] >= target and all(item["recall"] is None or item["recall"] >= target for item in measured["per_instruction"].values()):
            selected = {"threshold": threshold, "metrics": measured}
    return selected


def select():
    if (DIRECTORY / "selection.json").exists():
        raise SystemExit("Selection already frozen")
    specification = partition("development")
    actual = {case["identifier"]: case["reviews"] for case in specification["cases"]}
    candidates = []
    for path in sorted((DIRECTORY / "scores/development").glob("*.json")):
        artifact = json.loads(path.read_text())
        assert {result["identifier"] for result in artifact["results"]} == set(actual)
        points = {str(target): operating_point(artifact["results"], actual, target) for target in [0.9, 0.95, 0.99]}
        candidates.append({"name": artifact["name"], "operating_points": points, "scores_sha256": fingerprint(path),
                           "fixed_thresholds": {str(threshold): metrics(artifact["results"], actual, threshold) for threshold in [0.1, 0.25, 0.5, 0.75]}})
    assert len(candidates) == 6
    def ranking(candidate):
        measured = candidate["operating_points"]["0.95"]["metrics"]
        return (measured["reviews_avoided"], -measured["misses"], -len(candidate["name"]))
    trained = max([candidate for candidate in candidates if candidate["name"].startswith("epoch-")], key=ranking)
    pretrained = max([candidate for candidate in candidates if candidate["name"].startswith("pretrained-")], key=ranking)
    save(DIRECTORY / "selection.json", {"trained": trained["name"], "pretrained": pretrained["name"], "candidates": candidates,
         "manifest_sha256": fingerprint(DIRECTORY / "manifest.json"), "heldout_sha256": fingerprint(DIRECTORY / "heldout.json")})
    print(json.dumps({"trained": trained["name"], "pretrained": pretrained["name"], "candidates": [
        {"name": candidate["name"], "points": {target: {"threshold": point["threshold"],
         "skipped": point["metrics"]["reviews_avoided"], "missed": point["metrics"]["misses"]}
         for target, point in candidate["operating_points"].items()}} for candidate in candidates]}, indent=2))


def report(arguments):
    selection = json.loads((DIRECTORY / "selection.json").read_text())
    specification = partition(arguments.split)
    actual = {case["identifier"]: case["reviews"] for case in specification["cases"]}
    reports = []
    for candidate in selection["candidates"]:
        path = DIRECTORY / "scores" / arguments.split / (candidate["name"] + ".json")
        if not path.exists():
            continue
        artifact = json.loads(path.read_text())
        assert artifact["data_sha256"] == fingerprint(DIRECTORY / (arguments.split + ".json"))
        points = {}
        for target, point in candidate["operating_points"].items():
            points[target] = {"threshold": point["threshold"], "metrics": metrics(artifact["results"], actual, point["threshold"]),
                             "intervals": confidence_intervals(artifact["results"], actual, point["threshold"])}
        if arguments.split == "heldout":
            audit = json.loads((DIRECTORY / "authored-heldout.audit.json").read_text())
            ambiguous = {item["identifier"] for item in audit["ambiguous"]}
            original = {case["identifier"]: {case["instruction"]: case["review"]} for case in specification["cases"]}
            for target, point in candidate["operating_points"].items():
                points[target]["original_labels"] = metrics(artifact["results"], original, point["threshold"])
                points[target]["excluding_ambiguous"] = metrics(
                    [result for result in artifact["results"] if result["identifier"] not in ambiguous], actual, point["threshold"])
        reports.append({"name": candidate["name"], "operating_points": points,
                        "fixed_thresholds": {str(threshold): metrics(artifact["results"], actual, threshold) for threshold in [0.1, 0.25, 0.5, 0.75]}})
    save(DIRECTORY / (arguments.split + "-results.json"), reports)
    print(json.dumps([{ "name": report["name"], "points": {target: {key: value for key, value in point["metrics"].items()
                      if key not in {"errors", "per_instruction"}} for target, point in report["operating_points"].items()}}
                      for report in reports], indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=["prepare", "smoke", "train", "score", "select", "report"])
    parser.add_argument("--checkpoint", default="pretrained", choices=["pretrained", "epoch-1", "epoch-2"])
    parser.add_argument("--split", default="development", choices=["development", "historical", "heldout"])
    parser.add_argument("--examples", action="store_true")
    arguments = parser.parse_args()
    if arguments.action == "prepare":
        prepare()
    elif arguments.action in {"smoke", "train"}:
        train(arguments.action == "smoke")
    elif arguments.action == "score":
        score(arguments)
    elif arguments.action == "select":
        select()
    else:
        report(arguments)
