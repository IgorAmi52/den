import argparse
import json
import os
import pathlib
import time

from prepare import fingerprint, read_records


ENCODERS = {
    "bge-small": ("BAAI/bge-small-en-v1.5", "", 512),
    "modernbert": ("nomic-ai/modernbert-embed-base", "classification: ", 1024),
    "qwen": ("Qwen/Qwen3-Embedding-0.6B", "Instruct: Classify whether this developer exchange contains durable information worth preserving for future coding sessions.\nQuery: ", 1024),
}


def setup(directory):
    os.environ["HF_HOME"] = str(directory.parent / "huggingface")
    os.environ["TOKENIZERS_PARALLELISM"] = "false"
    os.environ["HF_HUB_DISABLE_PROGRESS_BARS"] = "1"
    import torch
    torch.set_num_threads(8)
    torch.manual_seed(42)
    return "mps" if torch.backends.mps.is_available() else "cpu"


def embed(arguments):
    device = setup(arguments.data)
    import numpy
    import torch
    from huggingface_hub import model_info
    from sentence_transformers import SentenceTransformer
    examples = read_records(arguments.data / "examples.jsonl")
    model_name, prefix, maximum_length = ENCODERS[arguments.encoder]
    revision = model_info(model_name).sha
    settings = {"attn_implementation": "sdpa"}
    configuration = {}
    if arguments.encoder == "modernbert":
        configuration["reference_compile"] = False
    model = SentenceTransformer(model_name, revision=revision, device=device, model_kwargs=settings, config_kwargs=configuration)
    model.max_seq_length = maximum_length
    texts = [prefix + example["text"] for example in examples]
    token_lengths = [len(tokens) for tokens in model.tokenizer(texts, truncation=False)["input_ids"]]
    started = time.monotonic()
    embeddings = model.encode(texts, batch_size=8, normalize_embeddings=True, show_progress_bar=False)
    if device == "mps":
        torch.mps.synchronize()
    seconds = time.monotonic() - started
    output = arguments.data / arguments.encoder
    output.mkdir(exist_ok=True)
    numpy.save(output / "embeddings.npy", embeddings)
    information = {"model": model_name, "revision": revision, "device": device, "prefix": prefix,
                   "maximum_length": maximum_length, "examples": len(examples), "seconds": seconds,
                   "milliseconds_per_example": seconds * 1000 / len(examples), "parameters": sum(parameter.numel() for parameter in model.parameters()),
                   "truncated_examples": sum(length > maximum_length for length in token_lengths),
                   "examples_sha256": fingerprint((arguments.data / "examples.jsonl").read_text())}
    (output / "encoder.json").write_text(json.dumps(information, indent=2))
    print(json.dumps(information, indent=2), flush=True)


def labeled_examples(directory):
    examples = read_records(directory / "examples.jsonl")
    labels = {label["identifier"]: label for label in read_records(directory / "labels.jsonl")}
    missing = [example["identifier"] for example in examples if example["identifier"] not in labels]
    if missing:
        raise ValueError(f"{len(missing)} examples still need labels")
    for example in examples:
        example["label"] = int(labels[example["identifier"]]["worthReview"])
    return examples


def metrics(expected, predicted):
    from sklearn.metrics import accuracy_score, balanced_accuracy_score, confusion_matrix, f1_score, precision_score, recall_score
    return {"accuracy": accuracy_score(expected, predicted), "balanced_accuracy": balanced_accuracy_score(expected, predicted),
            "macro_f1": f1_score(expected, predicted, average="macro", zero_division=0),
            "precision": precision_score(expected, predicted, zero_division=0), "recall": recall_score(expected, predicted, zero_division=0),
            "confusion_matrix": confusion_matrix(expected, predicted, labels=[0, 1]).tolist()}


def select_threshold(expected, scores, thresholds):
    best = None
    for threshold in thresholds:
        result = metrics(expected, scores >= threshold)
        key = (result["accuracy"], result["macro_f1"], -abs(float(threshold) - 0.5))
        if best is None or key > best[0]:
            best = (key, float(threshold), result)
    return best[1], best[2]


def fit(arguments):
    setup(arguments.data)
    import joblib
    import numpy
    from sklearn.linear_model import LogisticRegression
    examples = labeled_examples(arguments.data)
    labels = numpy.array([example["label"] for example in examples])
    training = numpy.array([example["split"] == "train" for example in examples])
    validation = numpy.array([example["split"] == "validation" for example in examples])
    embeddings = numpy.load(arguments.data / arguments.encoder / "embeddings.npy")
    information = json.loads((arguments.data / arguments.encoder / "encoder.json").read_text())
    if information["examples_sha256"] != fingerprint((arguments.data / "examples.jsonl").read_text()):
        raise ValueError("Embeddings belong to a different dataset")
    best = None
    for regularization in [0.1, 1, 10, 100]:
        classifier = LogisticRegression(C=regularization, max_iter=2000, random_state=42)
        classifier.fit(embeddings[training], labels[training])
        probabilities = classifier.predict_proba(embeddings)[:, 1]
        threshold, scores = select_threshold(labels[validation], probabilities[validation], numpy.linspace(0.1, 0.9, 33))
        key = (scores["accuracy"], scores["macro_f1"])
        if best is None or key > best[0]:
            best = (key, classifier, probabilities, threshold, scores, regularization)
    output = arguments.data / arguments.encoder
    joblib.dump(best[1], output / "classifier.joblib")
    numpy.save(output / "probabilities.npy", best[2])
    result = {"method": "logistic", "encoder": arguments.encoder, "threshold": best[3], "validation": best[4], "regularization": best[5]}
    (output / "classifier.json").write_text(json.dumps(result, indent=2))
    print(json.dumps(result), flush=True)
    similarities = embeddings @ embeddings[training].T
    training_labels = labels[training]
    numpy.savez(output / "router_examples.npz", embeddings=embeddings[training], labels=training_labels)
    best = None
    for neighbors in [1, 3, 5]:
        positive = numpy.sort(similarities[:, training_labels == 1], axis=1)[:, -neighbors:].mean(axis=1)
        negative = numpy.sort(similarities[:, training_labels == 0], axis=1)[:, -neighbors:].mean(axis=1)
        probabilities = (positive - negative + 1) / 2
        threshold, scores = select_threshold(labels[validation], probabilities[validation], numpy.linspace(0.4, 0.6, 81))
        key = (scores["accuracy"], scores["macro_f1"])
        if best is None or key > best[0]:
            best = (key, probabilities, threshold, scores, neighbors)
    numpy.save(output / "router_probabilities.npy", best[1])
    result = {"method": "semantic-router-style", "encoder": arguments.encoder, "threshold": best[2], "validation": best[3], "neighbors": best[4]}
    (output / "router.json").write_text(json.dumps(result, indent=2))
    print(json.dumps(result), flush=True)


def fine_tune(arguments):
    device = setup(arguments.data)
    import numpy
    from datasets import Dataset
    from setfit import SetFitModel, Trainer, TrainingArguments
    from sentence_transformers import SentenceTransformer
    from sklearn.linear_model import LogisticRegression
    examples = labeled_examples(arguments.data)
    training = [example for example in examples if example["split"] == "train"]
    information = json.loads((arguments.data / arguments.encoder / "encoder.json").read_text())
    output = arguments.data / (arguments.encoder + "-setfit")
    output.mkdir(exist_ok=True)
    body = SentenceTransformer(information["model"], revision=information["revision"], device=device,
                               config_kwargs={"reference_compile": False})
    model = SetFitModel(model_body=body, model_head=LogisticRegression(max_iter=2000, random_state=42), normalize_embeddings=True)
    model.model_body.max_seq_length = 512
    token_lengths = [len(tokens) for tokens in model.model_body.tokenizer(
        [information["prefix"] + example["text"] for example in examples], truncation=False)["input_ids"]]
    dataset = Dataset.from_dict({"text": [information["prefix"] + example["text"] for example in training], "label": [example["label"] for example in training]})
    settings = TrainingArguments(output_dir=str(output / "checkpoints"), batch_size=8, num_epochs=1,
                                 num_iterations=2, body_learning_rate=1e-5, max_length=512, seed=42,
                                 report_to="none", save_strategy="no", logging_steps=25, show_progress_bar=False)
    started = time.monotonic()
    trainer = Trainer(model=model, args=settings, train_dataset=dataset)
    trainer.train()
    (output / "training.json").write_text(json.dumps(trainer.st_trainer.state.log_history, indent=2))
    model.model_body.save(str(output / "model"))
    seconds = time.monotonic() - started
    started = time.monotonic()
    embeddings = model.model_body.encode([information["prefix"] + example["text"] for example in examples], normalize_embeddings=True, batch_size=8, show_progress_bar=False)
    encoding_seconds = time.monotonic() - started
    numpy.save(output / "embeddings.npy", embeddings)
    information.update({"fine_tuned": True, "device": device, "training_seconds": seconds, "training_examples": len(training), "maximum_length": 512,
                        "truncated_examples": sum(length > 512 for length in token_lengths),
                        "seconds": encoding_seconds, "milliseconds_per_example": encoding_seconds * 1000 / len(examples)})
    (output / "encoder.json").write_text(json.dumps(information, indent=2))
    arguments.encoder += "-setfit"
    fit(arguments)


def zero_shot(arguments):
    device = setup(arguments.data)
    import numpy
    import torch
    from huggingface_hub import model_info
    from transformers import AutoConfig, AutoModelForSequenceClassification, AutoTokenizer
    examples = read_records(arguments.data / "examples.jsonl")
    model_name = "MoritzLaurer/ModernBERT-base-zeroshot-v2.0"
    if arguments.encoder == "deberta":
        model_name = "MoritzLaurer/deberta-v3-base-zeroshot-v2.0-c"
    revision = model_info(model_name).sha
    tokenizer = AutoTokenizer.from_pretrained(model_name, revision=revision)
    configuration = AutoConfig.from_pretrained(model_name, revision=revision, reference_compile=False)
    model = AutoModelForSequenceClassification.from_pretrained(model_name, revision=revision, config=configuration).to(device).eval()
    entailment = next(int(identifier) for identifier, label in model.config.id2label.items() if label.lower() == "entailment")
    hypotheses = ["This exchange records a lasting project decision, useful rationale, constraint, or finding that a future coding session needs.",
                  "This exchange establishes a reusable workflow or a lasting preference about how agents should work.",
                  "This exchange establishes substantial ongoing work that clearly needs further sessions."]
    scores = []
    started = time.monotonic()
    with torch.inference_mode():
        for index in range(0, len(examples), 4):
            batch = examples[index:index + 4]
            premises = [example["text"] for example in batch for hypothesis in hypotheses]
            endings = hypotheses * len(batch)
            tokens = tokenizer(premises, endings, padding=True, truncation="only_first", max_length=1024, return_tensors="pt").to(device)
            probabilities = model(**tokens).logits.softmax(dim=-1)[:, entailment].cpu().numpy()
            scores.extend(probabilities.reshape(len(batch), len(hypotheses)).max(axis=1).tolist())
    output = arguments.data / "nli"
    if arguments.encoder == "deberta":
        output = arguments.data / "nli-deberta"
    output.mkdir(exist_ok=True)
    numpy.save(output / "probabilities.npy", numpy.array(scores))
    result = {"model": model_name, "revision": revision, "device": device, "hypotheses": hypotheses, "seconds": time.monotonic() - started,
              "examples_sha256": fingerprint((arguments.data / "examples.jsonl").read_text())}
    (output / "encoder.json").write_text(json.dumps(result, indent=2))
    print(json.dumps(result), flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=["embed", "fit", "setfit", "nli"])
    parser.add_argument("--data", type=pathlib.Path, required=True)
    parser.add_argument("--encoder", default="bge-small")
    arguments = parser.parse_args()
    {"embed": embed, "fit": fit, "setfit": fine_tune, "nli": zero_shot}[arguments.mode](arguments)
