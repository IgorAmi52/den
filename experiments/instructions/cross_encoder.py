import argparse
import hashlib
import importlib.util
import json
import os
import pathlib
import random
import sys
import time

sys.dont_write_bytecode = True
os.environ["TOKENIZERS_PARALLELISM"] = "false"
os.environ["HF_HUB_OFFLINE"] = "1"
os.environ["TRANSFORMERS_OFFLINE"] = "1"

import numpy
import torch
from transformers import AutoConfig, AutoModelForSequenceClassification, AutoTokenizer

DIRECTORY = pathlib.Path(".local/instruction-cross-encoder")
CACHE = pathlib.Path(".local/gate-experiment/huggingface/hub")
MODELS = {
    "minilm": ("cross-encoder/ms-marco-MiniLM-L6-v2", "233902d25c440f23af6f7d6e94d2946bac0bee0a"),
    "modernbert": ("MoritzLaurer/ModernBERT-base-zeroshot-v2.0", "d421c4545a438fd006fb43f8b981c5d908faa1e1"),
    "deberta": ("MoritzLaurer/deberta-v3-base-zeroshot-v2.0-c", "bddf8c5411c34ac3565e16e04384fd68b2618dda"),
}
HYPOTHESES = {
    "issues": "This exchange reports a concrete problem, or updates the status of a previously reported problem.",
    "deferred": "This exchange records a decision to postpone work, or reports completion or cancellation of previously postponed work.",
    "architecture": "This exchange establishes an architectural decision with its rationale or meaningful structural constraints.",
    "api": "This exchange identifies or updates a constraint imposed by an external API used by the project, or a workaround for that constraint.",
    "performance": "This exchange reports or corrects a measured performance regression or improvement and its cause.",
    "accessibility": "This exchange reports an accessibility barrier in the interface, or updates the status of a previously reported barrier.",
    "privacy": "This exchange reports exposure of credentials or personal information in application logs, or updates the status of a previously reported exposure.",
    "compatibility": "This exchange establishes or revises a commitment about reading older saved files or an agreed compatibility limit.",
}


def records(path):
    return [json.loads(line) for line in path.read_text().splitlines() if line.strip()]


def save(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2) + "\n")


def labels(directory):
    result = {record["identifier"]: {item["identifier"]: item["review"] for item in record["instructions"]}
              for record in records(directory / "labels.jsonl")}
    corrections = directory / "label-corrections.json"
    if corrections.exists():
        for correction in json.loads(corrections.read_text()):
            result[correction["identifier"]][correction["instruction"]] = correction["review"]
    return result


class PairEncoder:
    def __init__(self, name, context):
        torch.set_num_threads(4)
        torch.manual_seed(42)
        self.device = "mps" if torch.backends.mps.is_available() else "cpu"
        self.context = context
        self.descriptions = name.endswith("-descriptions")
        if self.descriptions:
            name = name.removesuffix("-descriptions")
        if name in MODELS:
            repository, revision = MODELS[name]
            path = CACHE / ("models--" + repository.replace("/", "--")) / "snapshots" / revision
        else:
            path = pathlib.Path(name)
        self.tokenizer = AutoTokenizer.from_pretrained(path, local_files_only=True)
        configuration = AutoConfig.from_pretrained(path, local_files_only=True, reference_compile=False)
        self.model = AutoModelForSequenceClassification.from_pretrained(path, config=configuration, local_files_only=True).to(self.device).eval()
        self.entailment = next((int(identifier) for identifier, label in configuration.id2label.items()
                               if label.lower() == "entailment"), None)
        self.parameters = sum(parameter.numel() for parameter in self.model.parameters())

    def encode(self, example, instruction):
        query = "Monitoring instruction: " + instruction["text"] + "\nExamples: " + " ".join(instruction["examples"])
        query = self.tokenizer.decode(self.tokenizer.encode(query, add_special_tokens=False)[:160], skip_special_tokens=True)
        text = "User: " + example["prompt"] + "\nAssistant: " + example["reply"]
        tokens = self.tokenizer.encode(text, add_special_tokens=False, truncation=False, verbose=False)
        previous = ""
        if self.context and example.get("previous"):
            previous = example["previous"]["prompt"] + "\n" + example["previous"]["reply"]
            previous = self.tokenizer.decode(self.tokenizer.encode(previous, add_special_tokens=False, verbose=False)[-64:], skip_special_tokens=True)
            previous = "\nPrevious context, not new evidence: " + previous
        encoded = []
        for position in range(0, max(1, len(tokens)), 192):
            passage = "New exchange: " + self.tokenizer.decode(tokens[position:position + 256], skip_special_tokens=True) + previous
            if self.entailment is None:
                encoded.append(self.tokenizer(query, passage, truncation="only_second", max_length=512))
            else:
                hypothesis = "The new exchange warrants review for this " + query
                if self.descriptions:
                    hypothesis = HYPOTHESES[instruction["identifier"]]
                encoded.append(self.tokenizer(passage, hypothesis, truncation="only_first", max_length=512))
        return encoded

    def logits(self, encoded):
        inputs = self.tokenizer.pad(encoded, padding=True, return_tensors="pt").to(self.device)
        logits = self.model(**inputs).logits
        if logits.shape[1] == 1:
            return logits[:, 0]
        others = [position for position in range(logits.shape[1]) if position != self.entailment]
        return logits[:, self.entailment] - torch.logsumexp(logits[:, others], dim=1)

    def score(self, examples, instructions):
        self.model.eval()
        scores = {instruction["identifier"]: [] for instruction in instructions}
        for instruction in instructions:
            groups = [self.encode(example, instruction) for example in examples]
            encoded = [window for group in groups for window in group]
            probabilities = []
            with torch.inference_mode():
                for position in range(0, len(encoded), 16):
                    probabilities.extend(self.logits(encoded[position:position + 16]).sigmoid().cpu().tolist())
            position = 0
            for group in groups:
                scores[instruction["identifier"]].append(max(probabilities[position:position + len(group)]))
                position += len(group)
        return scores


def score(arguments):
    if arguments.split == "test" and not (DIRECTORY / "selection.json").exists():
        raise SystemExit("Freeze selection before scoring the held-out sample.")
    directory = DIRECTORY / arguments.split
    examples = records(directory / "examples.jsonl")
    instructions = json.loads((directory / "instructions.json").read_text())
    started = time.monotonic()
    encoder = PairEncoder(arguments.model, arguments.context)
    startup = time.monotonic() - started
    started = time.monotonic()
    scores = encoder.score(examples, instructions)
    seconds = time.monotonic() - started
    timings = []
    for example in examples[:16]:
        started = time.monotonic()
        encoder.score([example], instructions)
        timings.append(time.monotonic() - started)
    result = {"model": arguments.model, "context": arguments.context, "scores": scores,
              "identifiers": [example["identifier"] for example in examples],
              "parameters": encoder.parameters, "device": encoder.device, "startup_seconds": startup,
              "scoring_seconds": seconds, "warm_median_seconds": float(numpy.median(timings)),
              "warm_p95_seconds": float(numpy.quantile(timings, .95))}
    save(directory / (arguments.name + ".json"), result)
    print(json.dumps({key: value for key, value in result.items() if key not in ("scores", "identifiers")}), flush=True)


def cosine(arguments):
    if arguments.split == "test" and not (DIRECTORY / "selection.json").exists():
        raise SystemExit("Freeze selection before scoring the held-out sample.")
    specification = importlib.util.spec_from_file_location("cosine_gate", "internal/brain/classifier.py")
    module = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(module)
    encoder = module.InstructionEncoder(CACHE / "models--BAAI--bge-small-en-v1.5/snapshots/5c38ec7c405ec4b44b94cc5a9bb96e735b38267a")
    directory = DIRECTORY / arguments.split
    examples = records(directory / "examples.jsonl")
    instructions = json.loads((directory / "instructions.json").read_text())
    texts = ["User: " + example["prompt"] + "\nAssistant: " + example["reply"] for example in examples]
    if arguments.context:
        for position, example in enumerate(examples):
            if example.get("previous"):
                previous = (example["previous"]["prompt"] + "\n" + example["previous"]["reply"])[-1600:]
                texts[position] = "Previous context: " + previous + "\nNew exchange\n" + texts[position]
    started = time.monotonic()
    scores = encoder.score(texts, instructions)
    save(directory / (arguments.name + ".json"), {"model": "cosine", "context": arguments.context,
         "identifiers": [example["identifier"] for example in examples], "scores": scores,
         "parameters": sum(parameter.numel() for parameter in encoder.model.parameters()),
         "scoring_seconds": time.monotonic() - started})


def train(arguments):
    if (DIRECTORY / "selection.json").exists():
        raise SystemExit("Model selection is frozen; start a new experiment before further training.")
    if (DIRECTORY / "models" / (arguments.name + "-epoch-1")).exists():
        raise SystemExit("Training artifacts already exist; choose a new run name.")
    directory = DIRECTORY / "train"
    actual = labels(directory)
    examples = records(directory / "examples.jsonl")
    instructions = json.loads((directory / "instructions.json").read_text())
    encoder = PairEncoder(arguments.model, arguments.context)
    if arguments.reset_head:
        encoder.model.classifier.reset_parameters()
    positives = {instruction["identifier"]: sum(actual[example["identifier"]][instruction["identifier"]] for example in examples)
                 for instruction in instructions}
    pairs = []
    for example in examples:
        for instruction in instructions:
            target = float(actual[example["identifier"]][instruction["identifier"]])
            count = positives[instruction["identifier"]] if target else len(examples) - positives[instruction["identifier"]]
            weight = len(examples) / (2 * count) if arguments.balanced else 1.
            pairs.append((encoder.encode(example, instruction), target, weight))
    optimizer = torch.optim.AdamW(encoder.model.parameters(), lr=2e-5, weight_decay=.01)
    generator = random.Random(42)
    started = time.monotonic()
    history = []
    for epoch in range(1, 4):
        generator.shuffle(pairs)
        losses = []
        encoder.model.train()
        for position in range(0, len(pairs), 8):
            batch = pairs[position:position + 8]
            optimizer.zero_grad()
            predictions = []
            for windows, _, _ in batch:
                predictions.append(encoder.logits(windows).max())
            targets = torch.tensor([target for _, target, _ in batch], device=encoder.device)
            weights = torch.tensor([weight for _, _, weight in batch], device=encoder.device)
            loss = torch.nn.functional.binary_cross_entropy_with_logits(torch.stack(predictions), targets, weight=weights)
            loss.backward()
            torch.nn.utils.clip_grad_norm_(encoder.model.parameters(), 1.)
            optimizer.step()
            losses.append(loss.item())
            if position % 256 == 0:
                print(json.dumps({"epoch": epoch, "pairs": position, "loss": float(numpy.mean(losses)),
                                  "seconds": time.monotonic() - started}), flush=True)
        output = DIRECTORY / "models" / (arguments.name + "-epoch-" + str(epoch))
        encoder.model.save_pretrained(output)
        encoder.tokenizer.save_pretrained(output)
        history.append({"epoch": epoch, "loss": float(numpy.mean(losses)), "seconds": time.monotonic() - started})
        save(output / "training.json", {"base": arguments.model, "revisions": MODELS, "context": arguments.context,
             "pairs": len(pairs), "seed": 42, "learning_rate": 2e-5, "balanced_by_instruction_and_label": arguments.balanced,
             "classification_head_reset": arguments.reset_head,
             "epochs": history, "objective": "BCE on maximum window logit per instruction/exchange; labels are not copied onto every window.",
             "input_sha256": {name: hashlib.sha256((directory / name).read_bytes()).hexdigest()
                              for name in ("examples.jsonl", "labels.jsonl", "label-corrections.json", "instructions.json")
                              if (directory / name).exists()}})
        development = records(DIRECTORY / "validation/examples.jsonl")
        all_instructions = json.loads((DIRECTORY / "validation/instructions.json").read_text())
        scores = encoder.score(development, all_instructions)
        save(DIRECTORY / "validation" / (arguments.name + "-epoch-" + str(epoch) + ".json"), {
             "model": str(output), "context": arguments.context, "scores": scores,
             "identifiers": [example["identifier"] for example in development], "parameters": encoder.parameters,
             "training": history})
        print(json.dumps(history[-1]), flush=True)


def measure(examples, actual, scores, threshold):
    actual_union = numpy.array([any(actual[example["identifier"]].values()) for example in examples])
    predicted = numpy.max(list(scores.values()), axis=0) >= threshold
    per_instruction = {}
    for identifier, values in scores.items():
        positive = numpy.array([actual[example["identifier"]][identifier] for example in examples])
        matched = numpy.array(values) >= threshold
        per_instruction[identifier] = {"positives": int(positive.sum()), "misses": int((positive & ~matched).sum()),
             "recall": float(matched[positive].mean()) if positive.any() else 1., "reviews": int(matched.sum())}
    return {"examples": len(examples), "positives": int(actual_union.sum()), "reviews": int(predicted.sum()),
            "misses": int((actual_union & ~predicted).sum()), "recall": float(predicted[actual_union].mean()),
            "unnecessary_reviews": int((predicted & ~actual_union).sum()),
            "reviews_avoided": int((~predicted).sum()), "savings": float((~predicted).mean()),
            "per_instruction": per_instruction}


def report(arguments):
    directory = DIRECTORY / arguments.split
    examples = records(directory / "examples.jsonl")
    actual = labels(directory)
    results = []
    for path in sorted(directory.glob("*.json")):
        artifact = json.loads(path.read_text())
        if not isinstance(artifact, dict) or "scores" not in artifact:
            continue
        if artifact["identifiers"] != [example["identifier"] for example in examples]:
            raise ValueError("Score/example ordering mismatch")
        values = sorted({value for values in artifact["scores"].values() for value in values})
        thresholds = [0.] + [(first + second) / 2 for first, second in zip(values, values[1:])]
        admissible = []
        for threshold in thresholds:
            metrics = measure(examples, actual, artifact["scores"], threshold)
            if metrics["recall"] >= .95 and all(item["recall"] >= .95 for item in metrics["per_instruction"].values()):
                admissible.append((metrics["savings"], threshold, metrics))
        _, threshold, metrics = max(admissible, key=lambda item: item[:2])
        results.append({"name": path.stem, "model": artifact["model"], "context": artifact["context"],
                        "parameters": artifact["parameters"], "threshold": threshold, "validation": metrics})
    print(json.dumps(results, indent=2), flush=True)
    save(DIRECTORY / "development-results.json", results)
    if arguments.action == "select":
        if (DIRECTORY / "selection.json").exists():
            raise SystemExit("Selection already frozen.")
        best = max(results, key=lambda result: (result["validation"]["savings"], -result["parameters"], result["threshold"]))
        fingerprints = {}
        model_path = pathlib.Path(best["model"])
        if model_path.exists():
            fingerprints = {str(path): hashlib.sha256(path.read_bytes()).hexdigest()
                            for path in sorted(model_path.iterdir()) if path.is_file()}
        save(DIRECTORY / "selection.json", {"selected": best, "comparison": results,
             "selected_artifact_sha256": fingerprints,
             "diagnostics_sha256": hashlib.sha256(pathlib.Path("experiments/instructions/cross_encoder_challenges.json").read_bytes()).hexdigest()})


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=["score", "cosine", "train", "report", "select"])
    parser.add_argument("--model", default="minilm")
    parser.add_argument("--name", default="minilm")
    parser.add_argument("--context", action="store_true")
    parser.add_argument("--balanced", action="store_true")
    parser.add_argument("--reset-head", action="store_true")
    parser.add_argument("--split", choices=["validation", "test"], default="validation")
    arguments = parser.parse_args()
    if arguments.action in ("report", "select") and arguments.split != "validation":
        raise SystemExit("Never calibrate on test labels.")
    {"score": score, "cosine": cosine, "train": train, "report": report, "select": report}[arguments.action](arguments)
