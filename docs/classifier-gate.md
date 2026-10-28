# Local classifier gate

## Decision, 2026-10-23

Use the validation-selected **ModernBERT SetFit encoder and logistic head**, threshold **0.625**, for live gate decisions. The user explicitly chose immediate deployment and accuracy over limiting review calls. Card proposals still come from the existing **gpt-6.1-sol** reviewer through Codex. Luna still handles progress and titles.

The original experiment measured 80.4% accuracy against strong-model labels, compared with Luna's 62.2%, on 312 held-out exchanges. This is not a measured live-system accuracy. See the [results](../experiments/gate/RESULTS.md) and [methodology](../experiments/gate/README.md). BGE SetFit remains a smaller challenger, not the validation-selected production model.

## Runtime behavior

- Every completed exchange reaches the classifier, including go-aheads and replies ending in a question. The two-reply spacing and ten-card-reviews-per-hour limits were removed. Review jobs still run one at a time. Five cards can be visible; additional proposals are saved and revalidated before appearing. See [card review and feedback](card-review.md).
- The model receives one input per exchange, using the exact training layout: the first user prompt as the goal (300 characters), the current user prompt's first 700 characters, and the reply's last 700 characters. Formatting is preserved. Historical title/folder placeholders and no waiting cards are deliberate parts of this input contract. Actual session, folder, waiting cards, and available exchanges are logged separately.
- A batch receives a review if **any** exchange reaches 0.625. Inputs are not concatenated into an untrained multi-exchange format, and all unread completed exchanges are scored.
- Approved plans, markdown writes, and pending cards force a review even when scores are low. The classifier still runs and its raw prediction is recorded. Pending cards bypass rejection because the training data did not adequately cover corrections or withdrawals.
- Missing dependencies, altered model artifacts, invalid output, or a 90-second inference timeout **allow the card review**. The failure is logged and shown in the UI. A local classifier failure does not count as a failed Sol call. The existing helper login/failure policy remains.
- Replies arriving during classification or card review remain available for another gate pass. Card reviews link to the positive gate calls they cover.

Go embeds `internal/brain/classifier.py` and launches a local Python process per gate batch. It loads the model offline, verifies its artifact hashes, uses Metal when available and CPU otherwise, and exits. This deliberately uses the model implementation already evaluated. It adds Python/PyTorch dependencies and model-loading latency; it is not a native Go inference engine or a persistent server.

The runtime lives in `${DEN_HOME:-$HOME/.den}/gate`: `venv/` and `model/`. The installed manifest identifies the model, tokenizer, head, metadata, and threshold together. Its initial version is `777693bd0923fbdcbc3054b10a33370ae4f5516763d5c3fa27552a0e945a375b`.

## Install or restore

The classifier is a separate installation from the DEN binary. The trained files currently exist in the maintainer's private archive; there is **no public model download**. Homebrew and a Git clone do not include them. A new installation of the current source still works without Python or the weights: it logs the unavailable classifier and allows direct model reviews, with more reviewer calls and potentially higher cost. The Homebrew formula currently pins `v0.2.0`, before this gate; a new release is needed to distribute the new code.

The pinned runtime installer supports **native Apple silicon macOS**, the platform used for validation. Intel Macs can use DEN with direct model reviews. Install `uv` first; the command below provisions Python 3.12 without relying on a system Python installation. Internet access is needed to obtain Python and the pinned packages. The model files are about 600 MB, and the environment and download caches need additional gigabytes.

Obtain the `modernbert-setfit` directory containing `encoder.json`, `classifier.json`, `classifier.joblib`, and the complete `model/` directory. The conversation dataset, embeddings, other candidate models and pretrained download cache are not needed for inference. Use only the trusted, version-matched artifacts; the classifier head is loaded through joblib.

From a source checkout containing this installer:

```sh
brew install uv
uv run --no-project --python 3.12 python experiments/gate/install.py --source /path/to/modernbert-setfit
make install
```

The installer verifies the required files against the checked-in SHA-256 inventory before downloading dependencies. It creates the environment, copies only runtime artifacts into a fresh temporary directory, and performs one offline prediction before activating the model. An incomplete, altered or unusable model is not activated. On failure, correct the reported error and rerun; an already-created Python environment can be reused.

It refuses to overwrite an installed model. Use `--destination /path/to/staged-gate` to prepare an update, then arrange the replacement with panels stopped. `DEN_HOME` controls the default destination (`$DEN_HOME/gate`) as well as runtime lookup; use the same value when installing and launching. Do not copy the virtual environment between machines: install it on the destination machine. Restart the DEN panel after installing a new binary; existing panels retain their loaded binary. The README explains restart and upgrade precautions.

`python3 experiments/gate/verify_artifacts.py` is a separate maintainer check of the **entire private experiment archive**, not a prerequisite for a new user with only the inference files.

## Logs and later labeling

Logs remain in `~/.den/repos/<repository>/calls.jsonl` and `decisions.jsonl`, alongside the existing suggestion records. Both positive and negative classifier decisions are retained.

| Record | Contents |
| --- | --- |
| `calls.jsonl`, `job: "gate check"` | Call identifier, session, completed prompt count, code version, policy version, duration, and zero token cost |
| Gate call `prompt` (a JSON string) | Exact `texts`, SHA-256 `inputHashes`, original exchange numbers, available session/exchange context, waiting cards, forced-review reason |
| Gate call `answer` | Per-exchange `scores`, `threshold`, model artifact `modelVersion`, device, raw `predictedReview`, final `worthReview`, reason, and `inferenceError` if any |
| `calls.jsonl`, `job: "card review"` | Full Sol request and answer, completed prompt count, and `gateCallIdentifiers` linking the triggering gate calls |
| Suggestions and `decisions.jsonl` | Full proposal snapshots, gate/reviewer call identifiers, replacements, revalidation, acceptance, rejection reasons, destination corrections and persistent card conversations |

Follow `gate call id → review.gateCallIdentifiers → review id → suggestion/decision.callId`. A review may cover multiple positive gate calls. First-run reviews can have no gate call. If logging fails, DEN reports the error; the review still proceeds. `den eval export` includes these logs and the runtime manifest/head metadata, but excludes the Python environment and encoder weight directory.

For the next training run, sample negative predictions as well as positive ones. Review both with the same durable-information criterion; Sol's output and user feedback are evidence, not automatic gate labels. A rejected card can reflect duplication or wording rather than an incorrect gate decision. Retain forced-review and failure flags so they are not mistaken for classifier positives.

Keep connected conversations in one split. Do not add the existing test examples to training and continue reporting the old test accuracy. Freeze a fresh test set before choosing a new model or threshold. Prioritize actual live excerpts, corrections/withdrawals, tentative versus confirmed decisions, and folder/skill/rule examples.

## Preserved artifacts

The main checkout stores the private archive under **`.local/gate-experiment/`**, ignored by Git. Public source control contains the scripts, dependency lock, reports, and SHA-256 inventory in `experiments/gate/artifacts.json`; it contains no conversation dataset or weights.

- `data/`: all 1,843 examples with fixed splits, labels, independent audit, baseline and sensitivity calls, model-call prompts/answers/events, features, predictions, fitted heads, both fine-tuned encoders, metadata, error analysis, results, and environment versions.
- `huggingface/`: the downloaded pretrained models and tokenizers, retained so comparison models can be restored without relying on `/tmp`.
- The original independent copy is still at `~/.den/repos/-Users-igor-amidzic-GolandProjects-den/folders/04ede3d9/gate-experiment`.

The archive retains the exact model inputs used, not an independent backup of every original agent transcript. Source transcript paths are recorded in the dataset. These are local copies on one Mac, not an off-device backup; copy the ignored directory explicitly when moving the project.

To rerun the comparison without new labeling or training:

```sh
uv venv --python 3.12 .local/gate-experiment/venv
uv pip install --python .local/gate-experiment/venv/bin/python -r experiments/gate/requirements.lock.txt
.local/gate-experiment/venv/bin/python experiments/gate/evaluate.py --data .local/gate-experiment/data
```

Evaluation rewrites `results.json` and `errors.jsonl`. Preserve the archived originals before changing the experiment or starting a new run. To recover any previous model call or benchmark, use the stored records rather than paying to label the same data again.
