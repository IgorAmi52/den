import hashlib
import json
import os
import pathlib
import sys

os.environ["HF_HUB_OFFLINE"] = "1"
os.environ["TRANSFORMERS_OFFLINE"] = "1"
os.environ["TOKENIZERS_PARALLELISM"] = "false"

import joblib
import torch
from sentence_transformers import SentenceTransformer


directory = pathlib.Path(sys.argv[1])
manifest = json.loads((directory / "manifest.json").read_text())
if hashlib.sha256(json.dumps(manifest["files"], sort_keys=True).encode()).hexdigest() != manifest["version"]:
    raise ValueError("Classifier manifest version does not match its artifact hashes")
for relative_path, expected_hash in manifest["files"].items():
    with (directory / relative_path).open("rb") as artifact:
        if hashlib.file_digest(artifact, "sha256").hexdigest() != expected_hash:
            raise ValueError(f"Classifier artifact changed: {relative_path}")

torch.set_num_threads(8)
device = "mps" if torch.backends.mps.is_available() else "cpu"
encoder = json.loads((directory / "encoder.json").read_text())
settings = json.loads((directory / "classifier.json").read_text())
model = SentenceTransformer(str(directory / "model"), device=device, local_files_only=True,
                            config_kwargs={"reference_compile": False})
model.max_seq_length = encoder["maximum_length"]
request = json.load(sys.stdin)
embeddings = model.encode([encoder["prefix"] + text for text in request["texts"]],
                          normalize_embeddings=True, batch_size=8, show_progress_bar=False)
scores = joblib.load(directory / "classifier.joblib").predict_proba(embeddings)[:, 1]
print(json.dumps({"scores": scores.tolist(), "threshold": settings["threshold"],
                  "modelVersion": manifest["version"], "device": device}, allow_nan=False))
