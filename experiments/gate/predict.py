import argparse
import json
import pathlib
import sys

import joblib

from train import setup


def score_texts(directory, texts, router=False):
    device = setup(directory.parent)
    from sentence_transformers import SentenceTransformer
    encoder = json.loads((directory / "encoder.json").read_text())
    settings_name = "classifier.json"
    if router:
        settings_name = "router.json"
    classifier = json.loads((directory / settings_name).read_text())
    source = str(directory / "model")
    if not (directory / "model").exists():
        source = encoder["model"]
    model = SentenceTransformer(source, revision=encoder["revision"], device=device, local_files_only=True,
                                config_kwargs={"reference_compile": False})
    model.max_seq_length = encoder["maximum_length"]
    embeddings = model.encode([encoder["prefix"] + text for text in texts], normalize_embeddings=True, batch_size=8, show_progress_bar=False)
    if router:
        import numpy
        examples = numpy.load(directory / "router_examples.npz")
        similarities = embeddings @ examples["embeddings"].T
        neighbors = classifier["neighbors"]
        positive = numpy.sort(similarities[:, examples["labels"] == 1], axis=1)[:, -neighbors:].mean(axis=1)
        negative = numpy.sort(similarities[:, examples["labels"] == 0], axis=1)[:, -neighbors:].mean(axis=1)
        scores = (positive - negative + 1) / 2
    else:
        scores = joblib.load(directory / "classifier.joblib").predict_proba(embeddings)[:, 1]
    return scores, classifier["threshold"]


def predict(directory, text, router=False):
    scores, threshold = score_texts(directory, [text], router)
    return {"worthReview": bool(scores[0] >= threshold), "score": float(scores[0]), "threshold": threshold}


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--model", type=pathlib.Path, required=True)
    parser.add_argument("--router", action="store_true")
    arguments = parser.parse_args()
    print(json.dumps(predict(arguments.model, sys.stdin.read(), arguments.router)))
