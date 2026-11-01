# Instruction gate

Saved Den instructions replace the old retention classifier. The local runtime supports the trained MiniLM cross-encoder, using only the new exchange. It is the user's chosen interim gate, not a validated general-purpose default. Without a local installation, Den uses direct Sol review. The original ModernBERT experiment remains in [experiments/gate](../experiments/gate/RESULTS.md); its accuracy answers a different question.

## Runtime

The checkpoint is `trained-new-epoch-3`: 22.7M parameters, CPU FP32, threshold `0.005811875686049461`. It skipped 37/160 historical exchanges and missed 2/88 relevant exchanges with four instructions active. Per-instruction recall is weaker, especially for deferred work and API constraints. These reused results do not establish reliability for arbitrary instructions or savings in production. See the [cross-encoder report](../experiments/instructions/CROSS_ENCODER_RESULTS.md) and [integration verification](../experiments/instructions/RUNTIME_INTEGRATION.md).

- Select active instructions for the source session. Repo is the default scope; folder scope includes descendants. No applicable instruction means no new proposals. Pending legacy cards can still receive maintenance review.
- Preserve complete user and assistant text. Process at most ten new exchanges per batch; later exchanges remain queued. The gate sees only new exchanges. The Sol reviewer separately receives preceding context to understand references.
- Without an installed gate, use direct review normally. Missing approved examples also allow direct review. A paused instruction does not participate. Instructions and examples changing during a gate/review invalidate the in-flight result.
- The trained gate pairs each instruction and its approved examples (up to 160 tokens) with 256-token passages, overlapping by 64 tokens. It takes the maximum sigmoid score across passages. A match for any instruction permits a single Sol review of all applicable instructions. Scores are thresholded model outputs, not calibrated probabilities.
- A local Python worker stays loaded across calls and uses four CPU threads. A changed manifest restarts it. Artifact verification failures, invalid output, crashed workers, or a 90-second deadline allow direct review; actual inference errors are logged and shown.
- The legacy `~/.den/gate` model is not used by this feature. Its compatible Python environment can be reused. Approved plans, Markdown writes, and old automatic note/skill rules do not bypass instruction relevance.

## Experimental installation

Den works without Python or model weights. To install the evaluated trained checkpoint, install `uv`, restore the private checkpoint, then run:

```sh
python3 experiments/instructions/install.py --source /path/to/trained-new-epoch-3
```

The default source is `.local/instruction-cross-encoder/models/trained-new-epoch-3` when present. The installer verifies every checkpoint file against `trained_release.json`; it does not substitute an untrained public model. Weights are not distributed in this repository. Installation creates a Python environment under `${DEN_HOME:-$HOME/.den}/instruction-gate` and refuses to replace an existing installation. `--destination` stages an isolated install; `--runtime` reuses a compatible Python environment. Offline inference is verified before activation.

The earlier BGE cosine experiment remains available with `--model cosine`; that explicitly downloads its pinned public encoder and uses threshold 0.51. Its high-recall configuration saved essentially no reviews with all four original evaluation instructions active. It is not the trained gate.

## Traceability

Gate logs contain complete matching input, input hashes, instruction identifiers/revisions/examples, per-instruction/per-exchange scores, matched identifiers, threshold, model version, timing, fallback reason and inference errors. Model usage is zero for local inference. Reviews link to their triggering gate calls. Proposals carry their instruction identifier/title/revision, source evidence, destination and context versions.

Rejections caused by placement or wording are not negative relevance labels. Evaluation exports include instruction files and the runtime manifest, excluding embedding weights and the Python environment. Private historical transcripts, labels and model responses remain in ignored `.local/` directories.
