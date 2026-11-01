# Instruction feature evaluation

Read [RESULTS.md](RESULTS.md) before enabling the experimental gate. The feature works with direct reviews and no local model install. Historical source data and model outputs stay under ignored `.local/` directories.

The runtime now supports the trained MiniLM new-exchange-only checkpoint as the user-approved interim gate; see [integration verification](RUNTIME_INTEGRATION.md). Comparison reports include [standalone Decider questions](STANDALONE_QUESTION_RESULTS.md), [GLiClass fine-tuning](GLICLASS_FINETUNING_RESULTS.md), [pretrained GLiClass and Qwen](PRETRAINED_GATE_RESULTS.md), and [cross-encoder training](CROSS_ENCODER_RESULTS.md). Integration preserves the evaluated weights, input format, and threshold.

## Historical sample and labels

```sh
python3 experiments/instructions/prepare.py --destination .local/instruction-evaluation
python3 experiments/instructions/label.py --data .local/instruction-evaluation
```

Preparation refuses to overwrite a frozen sample. Labeling resumes from cached results and uses the authenticated Codex CLI with Sol at high effort. Review positive and negative labels; record justified corrections separately in `label-corrections.json`.

Use a Python 3.12 environment with the dependencies pinned in `experiments/gate/requirements.lock.txt`. Candidate weights are restored from the existing private experiment archive. Score all candidates, then freeze selection before revealing test results:

```sh
python experiments/instructions/evaluate.py score --model bge-small
python experiments/instructions/evaluate.py score --model modernbert-setfit
python experiments/instructions/evaluate.py score --model qwen
python experiments/instructions/evaluate.py score --model bge-small-behavior
python experiments/instructions/evaluate.py select
python experiments/instructions/evaluate.py test
```

`evaluate.py` has maintainer-local model paths; change those paths for another machine without changing the frozen sample or interpreting tuned results as held-out. The existing fine-tuned encoder's original training groups must remain excluded.

## Runtime and reviewer checks

```sh
go build -o .local/instruction-replay-runner ./experiments/instructions/replay
.local/instruction-replay-runner --checks
.local/instruction-replay-runner
.local/instruction-replay-runner --chat
```

The first command runs local behavior checks. The reviewer and chat runs call Sol in isolated workspaces and record usage. Requests are cached by their complete prompt/schema/files signature. Cached answers are passed through current validation again.

For the optional local runtime, install to an isolated destination first:

```sh
python3 experiments/instructions/install.py --destination .local/instruction-runtime/instruction-gate
.local/instruction-replay-runner --gate --runtime-home .local/instruction-runtime
```

`--source /path/to/trained-new-epoch-3` supplies the private evaluated checkpoint; all files must match `trained_release.json`. `--model cosine` explicitly selects the earlier public BGE experiment; `--runtime /path/to/venv` reuses compatible dependencies. Runtime checks temporarily alter only the isolated manifest to verify failure behavior, then restore it. Do not point these checks at a live runtime.

No command above installs or replaces the Den binary. Build the branch with `go build -o .local/den-instructions .`; the user's installed binary remains unchanged until explicitly installed.

## Cross-encoder experiment

The follow-up [cross-encoder report](CROSS_ENCODER_RESULTS.md) compares pretrained pair models with instruction-conditioned fine-tuning. These scripts do not install a production gate.

Use the same pinned Python dependencies as above. The model identifiers and exact revisions are in `cross_encoder.py`; restore their Hugging Face snapshots under `.local/gate-experiment/huggingface/hub`. On the development machine, the compatible interpreter is `/Users/igor.amidzic/.den/gate/venv/bin/python`.

```sh
python experiments/instructions/prepare_cross_encoder.py
python3 experiments/instructions/label.py --data .local/instruction-cross-encoder/train
python3 experiments/instructions/label.py --data .local/instruction-cross-encoder/test
```

Preparation excludes all previous instruction-evaluation groups from the new training and test partitions. It freezes 472 training exchanges, reuses the previous 192 exchanges for validation, and reserves 160 exchanges from 61 other conversation groups for testing. Missing historical sources are excluded before selection. Test labeling may run independently, but do not inspect those labels before selecting the model. Label corrections belong in the relevant partition's `label-corrections.json`.

The saved experiment labels predate the added clarification about deferred work. Auditing found that unaccepted deferral suggestions, routine task ordering and approval/access blockers were being counted as postponements. Corrections preserve the original labels and reasons; cached labeling calls are retained. Fresh labeling uses the clarified policy and will not reproduce the original raw labels exactly.

For the saved run, the sealed test examples were also labeled under that clarified policy in `.local/instruction-cross-encoder/test-clarified`. Its `examples.jsonl` and `instructions.json` are exact copies of `test`; run the same label command against that directory. `evaluate_cross_encoder.py` uses these as primary labels and reports the original test labels as a sensitivity check. This choice was recorded before test labels or predictions were inspected.

```sh
python experiments/instructions/cross_encoder.py cosine --name cosine-context --context
python experiments/instructions/cross_encoder.py cosine --name cosine-new
python experiments/instructions/cross_encoder.py score --model minilm --name minilm-new
python experiments/instructions/cross_encoder.py score --model minilm --name minilm-context --context
python experiments/instructions/cross_encoder.py score --model modernbert --name modernbert-new
python experiments/instructions/cross_encoder.py score --model deberta --name deberta-new
python experiments/instructions/cross_encoder.py score --model modernbert-descriptions --name modernbert-descriptions
python experiments/instructions/cross_encoder.py score --model deberta-descriptions --name deberta-descriptions
python experiments/instructions/cross_encoder.py train --model minilm --name trained-context --context
python experiments/instructions/cross_encoder.py train --model minilm --name trained-new
python experiments/instructions/cross_encoder.py train --model minilm --name balanced-context --context --balanced
python experiments/instructions/cross_encoder.py train --model minilm --name balanced-reset-context --context --balanced --reset-head
python experiments/instructions/cross_encoder.py report
python experiments/instructions/cross_encoder.py select
```

Each training run saves three checkpoints and their validation scores. Selection maximizes reviews avoided subject to at least 95% recall both overall and for every instruction enabled alone. A single shared threshold is calibrated on validation; there are no instruction-specific thresholds. Training and selection refuse to overwrite completed runs or a frozen selection.

The balanced variant weights each instruction's positive and negative examples equally in the loss. This addresses different class frequencies without needing separate thresholds for the four instructions. All weights come from training labels only.

The final variant also reinitializes the small classification head, retaining the pretrained transformer. This checks whether the inherited search-ranking head hinders adaptation to review relevance.

After selection, score the selected checkpoint and frozen comparison candidates with the same names, model paths and context flags, adding `--split test`. Then run:

```sh
python experiments/instructions/evaluate_cross_encoder.py historical
python experiments/instructions/evaluate_cross_encoder.py diagnostics
```

Historical evaluation uses frozen thresholds and reports conversation-group bootstrap intervals. Diagnostics use the separately authored `cross_encoder_challenges.json`, including four unseen instructions, and replay the gate against the previously cached 16 Sol reviewer scenarios. That replay is a composition check, not a new live Sol run or a held-out estimate of proposal quality. Private errors, labels, token usage, weights and scores remain under `.local/instruction-cross-encoder`.

## Pretrained GLiClass and Qwen experiment

The [pretrained comparison report](PRETRAINED_GATE_RESULTS.md) covers calibration on 192 development exchanges, the reused 160 historical exchanges, 160 newly authored cases across 12 instructions, and the existing 48 diagnostics. This round trains no weights and makes no new Sol proposal-review calls.

Run from the repository root. Create an isolated Python 3.12 environment and download the exact model snapshots:

```sh
python3.12 -m venv .local/pretrained-instruction-gate/venv
.local/pretrained-instruction-gate/venv/bin/python -m pip install -r experiments/instructions/pretrained-requirements.lock.txt
.local/pretrained-instruction-gate/venv/bin/hf download knowledgator/gliclass-v4-edge --revision 885af1af8a07edf4ed34df83ca0d995ed8201283 --cache-dir .local/pretrained-instruction-gate/huggingface
.local/pretrained-instruction-gate/venv/bin/hf download Qwen/Qwen3-Reranker-0.6B --revision e61197ed45024b0ed8a2d74b80b4d909f1255473 --cache-dir .local/pretrained-instruction-gate/huggingface
```

Scoring reads cached snapshots offline. Restore the private `validation`, `test`, `test-clarified`, and `models/trained-new-epoch-3` directories under `.local/instruction-cross-encoder/`. Restore `fresh-authored/` and `experiment-policy.json` under `.local/pretrained-instruction-gate/`. The fresh directory must include `instructions.json`, `examples.jsonl`, `labels.jsonl`, and `label-audit.json`; keep its manifest and provenance artifacts too. The public [synthetic fixture](pretrained_gate_challenges.json) preserves the fresh cases and labels for inspection, but this script reads the archived files, not that export. There is no automatic preparation command for this round, and a public checkout alone cannot reproduce the private historical comparison.

The commands below replay the three finalists. They require a clean experiment output directory with the input artifacts restored; completed score files and frozen selection refuse overwrite. Preserve the original run when reproducing. The previous MiniLM checkpoint uses the older compatible environment from `experiments/gate/requirements.lock.txt`, available here at `/Users/igor.amidzic/.den/gate/venv/bin/python`.

```sh
.local/pretrained-instruction-gate/venv/bin/python experiments/instructions/pretrained_gate.py score --model gliclass --format simple --context --precision bfloat16 --device mps --name gliclass-metal-simple-context
.local/pretrained-instruction-gate/venv/bin/python experiments/instructions/pretrained_gate.py score --model qwen --precision bfloat16 --device mps --name qwen-metal-new
/Users/igor.amidzic/.den/gate/venv/bin/python experiments/instructions/pretrained_gate.py score --model trained-minilm --name trained-minilm-new
.local/pretrained-instruction-gate/venv/bin/python experiments/instructions/pretrained_gate.py select --candidates gliclass-metal-simple-context qwen-metal-new trained-minilm-new
```

Metal commands require an Apple GPU accessible to the process. On this machine the execution sandbox hides Metal, so GPU runs required approved execution outside it. CPU FP32 is a different configuration: use a different name and recalibrate rather than applying the BF16 thresholds. The report includes CPU timing and memory measurements; neither CPU BF16 nor the trial INT8 conversion was a useful deployment option.

To reproduce the earlier format comparison before selection, score `gliclass-new` with defaults, `gliclass-joint-new` with `--format joint`, `gliclass-simple-context` with `--format simple --context`, `qwen-new` with `--model qwen`, and `qwen-simple-context` with `--model qwen --format simple --context`. These use CPU FP32. Include those names in `select --candidates` alongside the finalists. Finalists were chosen from development results separately for each pretrained model family; `experiment-policy.json` records that choice. The selection file's overall development winner is the old MiniLM baseline, which is not a recommendation to deploy it.

After selection, repeat each finalist's exact scoring command with `--split historical`, then `--split fresh`, then `--split diagnostics`. Report the frozen thresholds without retuning:

```sh
.local/pretrained-instruction-gate/venv/bin/python experiments/instructions/pretrained_gate.py report --split historical
.local/pretrained-instruction-gate/venv/bin/python experiments/instructions/pretrained_gate.py report --split fresh
.local/pretrained-instruction-gate/venv/bin/python experiments/instructions/pretrained_gate.py report --split diagnostics
```

For dedicated timing, replace `score` with `benchmark` in each finalist command and run one process at a time. It measures the same first 16 development exchanges with four active instructions. Outputs, source hashes, label audits, score files, frozen selection, sensitivity results, and benchmarks remain under `.local/pretrained-instruction-gate/`. All current evaluation sets have now been inspected; future tuning needs new held-out material.

## Bounded GLiClass fine-tuning

Read [the results and limitations](GLICLASS_FINETUNING_RESULTS.md) before using these weights. The [public synthetic fixture](gliclass_finetuning_challenges.json) preserves the new training, development, and evaluation cases with original and audited labels; reproduction still uses the private frozen archives below.

`finetune_gliclass.py` uses the same pinned pretrained environment and cached GLiClass revision. It keeps the first 20 encoder layers and embeddings frozen, updating the last four layers plus classification/calibration components. Training uses FP32 parameters with BF16 Metal autocasting; evaluation reloads checkpoints in BF16. The deployed model architecture stays unchanged.

Restore the private historical partitions listed above and the three `authored-{training,development,heldout}.json` files with their corresponding `.audit.json` files under `.local/gliclass-finetuning/`. Preparation preserves raw labels, applies independent audit corrections, excludes ambiguous training cases, checks partition separation, and freezes source hashes. Completed datasets, checkpoints, scores, and selection must be preserved; use a clean experiment directory for reproduction.

```sh
.local/pretrained-instruction-gate/venv/bin/python experiments/instructions/finetune_gliclass.py prepare
.local/pretrained-instruction-gate/venv/bin/python experiments/instructions/finetune_gliclass.py smoke
.local/pretrained-instruction-gate/venv/bin/python experiments/instructions/finetune_gliclass.py train
```

The smoke check performs one disposable update without saving weights. The actual run starts again from the pretrained model: two epochs, 960 sampled instruction/exchange pairs per epoch, batch size eight, learning rate 0.00001, seed 42. Sampling balances instruction and label buckets. It does not visit every available pair; training history records coverage. Approved positive examples are included on half of sampled pairs. For long exchanges, the strongest passage is selected without gradients, then used for the training update; positive exchange labels are not copied onto every passage.

Score `pretrained`, `epoch-1`, and `epoch-2` on development, each with examples absent and present:

```sh
for checkpoint in pretrained epoch-1 epoch-2; do
  .local/pretrained-instruction-gate/venv/bin/python experiments/instructions/finetune_gliclass.py score --checkpoint "$checkpoint"
  .local/pretrained-instruction-gate/venv/bin/python experiments/instructions/finetune_gliclass.py score --checkpoint "$checkpoint" --examples
done
.local/pretrained-instruction-gate/venv/bin/python experiments/instructions/finetune_gliclass.py select
```

Selection compares the six development configurations and separately freezes the best pretrained and trained candidates. A shared threshold must retain at least 95% recall overall and for every instruction. The script also freezes thresholds for 90% and 99% targets and reports fixed score cutoffs; scores are not calibrated probabilities.

After selection, score only those two finalists, keeping their `--checkpoint` and `--examples` settings, first with `--split heldout` and then `--split historical`. Run `report --split heldout` and `report --split historical` with the same interpreter and script. Reports include per-instruction errors and original-label/ambiguity sensitivity for held-out cases. Historical results reuse the earlier comparison set; the new synthetic set evaluates its designated instruction alone. Neither establishes production cost savings or Sol proposal quality. Nothing installs these weights into Den.
