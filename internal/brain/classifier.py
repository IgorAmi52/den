import hashlib
import json
import os
import pathlib
import sys

os.environ["HF_HUB_OFFLINE"] = "1"
os.environ["TRANSFORMERS_OFFLINE"] = "1"
os.environ["TOKENIZERS_PARALLELISM"] = "false"

import numpy
import torch
from sentence_transformers import SentenceTransformer
from transformers import AutoConfig, AutoModelForSequenceClassification, AutoTokenizer


class InstructionCrossEncoder:
    def __init__(self, model_path):
        torch.set_num_threads(4)
        self.device = "cpu"
        self.tokenizer = AutoTokenizer.from_pretrained(model_path, local_files_only=True)
        configuration = AutoConfig.from_pretrained(model_path, local_files_only=True, reference_compile=False)
        self.model = AutoModelForSequenceClassification.from_pretrained(
            model_path, config=configuration, local_files_only=True).to(self.device).float().eval()

    def score(self, texts, instructions):
        passages = []
        for text in texts:
            tokens = self.tokenizer.encode(text, add_special_tokens=False, truncation=False, verbose=False)
            passages.append(["New exchange: " + self.tokenizer.decode(tokens[position:position + 256], skip_special_tokens=True)
                             for position in range(0, max(1, len(tokens)), 192)])
        scores = {}
        for instruction in instructions:
            query = "Monitoring instruction: " + instruction["text"] + "\nExamples: " + " ".join(instruction["examples"])
            query = self.tokenizer.decode(self.tokenizer.encode(query, add_special_tokens=False)[:160], skip_special_tokens=True)
            encoded = [self.tokenizer(query, passage, truncation="only_second", max_length=512)
                       for group in passages for passage in group]
            probabilities = []
            with torch.inference_mode():
                for position in range(0, len(encoded), 16):
                    inputs = self.tokenizer.pad(encoded[position:position + 16], padding=True, return_tensors="pt")
                    probabilities.extend(self.model(**inputs).logits[:, 0].sigmoid().tolist())
            position = 0
            scores[instruction["identifier"]] = []
            for group in passages:
                scores[instruction["identifier"]].append(max(probabilities[position:position + len(group)]))
                position += len(group)
        return scores


class InstructionEncoder:
    def __init__(self, model_path, prefix="", device=None):
        torch.set_num_threads(4)
        self.device = device or ("mps" if torch.backends.mps.is_available() else "cpu")
        self.model = SentenceTransformer(str(model_path), device=self.device, local_files_only=True,
                                         config_kwargs={"reference_compile": False})
        self.model.max_seq_length = 512
        self.prefix = prefix
        self.examples = {}

    def passages(self, text):
        tokens = self.model.tokenizer.encode(text, add_special_tokens=False, truncation=False)
        return [self.model.tokenizer.decode(tokens[position:position + 256], skip_special_tokens=True)
                for position in range(0, max(len(tokens), 1), 192)]

    def embed(self, passages):
        return self.model.encode([self.prefix + passage for passage in passages], normalize_embeddings=True,
                                 batch_size=16, show_progress_bar=False)

    def score(self, texts, instructions):
        passage_groups = [self.passages(text) for text in texts]
        passages = [passage for group in passage_groups for passage in group]
        embeddings = self.embed(passages)
        scores = {}
        for instruction in instructions:
            examples = tuple(instruction["examples"])
            if examples not in self.examples:
                if len(self.examples) >= 256:
                    self.examples.clear()
                self.examples[examples] = self.embed([passage for example in examples for passage in self.passages(example)])
            similarities = embeddings @ self.examples[examples].T
            position = 0
            instruction_scores = []
            for group in passage_groups:
                instruction_scores.append(float(numpy.max(similarities[position:position + len(group)])))
                position += len(group)
            scores[instruction["identifier"]] = instruction_scores
        return scores


def serve(directory):
    manifest = json.loads((directory / "manifest.json").read_text())
    for relative_path, expected_hash in manifest["files"].items():
        with (directory / relative_path).open("rb") as artifact:
            if hashlib.file_digest(artifact, "sha256").hexdigest() != expected_hash:
                raise ValueError(f"Embedding artifact changed: {relative_path}")
    if manifest.get("kind") == "cross-encoder":
        encoder = InstructionCrossEncoder(directory / "model")
    elif manifest.get("kind", "cosine") == "cosine":
        encoder = InstructionEncoder(directory / "model", manifest.get("prefix", ""))
    else:
        raise ValueError("Unknown instruction gate kind")
    version = hashlib.sha256((directory / "manifest.json").read_bytes()).hexdigest()
    for line in sys.stdin:
        request = json.loads(line)
        scores = encoder.score(request["texts"], request["instructions"])
        maximum_scores = [max(values[position] for values in scores.values()) for position in range(len(request["texts"]))]
        print(json.dumps({"scores": maximum_scores, "instructionScores": scores,
                          "threshold": manifest["threshold"], "modelVersion": version,
                          "device": encoder.device}, allow_nan=False), flush=True)


if __name__ == "__main__":
    serve(pathlib.Path(sys.argv[1]))
