# Local cross-encoder gate — 2026-10-28

**Fine-tuning improves substantially on the existing cosine gate, but none of the tested models meets the reliability requirements for enabling a general user-instruction gate.** The validation-selected model skips 47 of 160 held-out reviews (29.4%), but misses 5 of 88 relevant exchanges. It also fails the per-instruction recall target and still triggers on old context. Keep direct review as the default.

The useful next baseline is the model trained on the new exchange alone. It skips 37/160 reviews, misses 2/88 relevant exchanges, avoids all eight synthetic stale-context triggers, and preserves all 16 cached reviewer scenario outcomes. This is a direction for further work, **not a newly selected or validated production winner**: its API and deferred-work recall also fails the target.

## Held-out results

All thresholds were selected on development data and frozen before test labels or predictions were inspected. The criterion was maximum reviews avoided with at least 95% recall overall **and for every instruction enabled alone**.

| Gate | Reviews skipped / 160 | Relevant exchanges caught / 88 | Recall |
| --- | ---: | ---: | ---: |
| Existing BGE cosine, with previous context | 2 (1.3%) | 88 | 100% |
| BGE cosine, new exchange only | 0 | 88 | 100% |
| Pretrained MiniLM cross-encoder | 0 | 88 | 100% |
| Fine-tuned MiniLM, ordinary training + context | 40 (25.0%) | 87 | 98.9% |
| Fine-tuned MiniLM, ordinary training, new exchange only | 37 (23.1%) | 86 | 97.7% |
| **Fine-tuned MiniLM, balanced training + context — preselected** | **47 (29.4%)** | **83** | **94.3%** |

Skipped reviews include missed relevant exchanges. The selected model correctly filters 42 of the 72 irrelevant exchanges and still sends 30 irrelevant exchanges to review. Its savings have a conversation-group bootstrap 95% interval of **19.9–39.9%**. Compared with the existing cosine gate, it avoids 45 additional reviews, including five additional misses.

These are isolated exchange checks with all four instructions active, not measured production spending. Buffered exchanges, pending-card maintenance, missing examples and failure fallbacks can reduce actual savings.

### Each instruction enabled alone

The combined gate can hide a weak instruction because another instruction lets the exchange through. These counts therefore matter more than the overall recall alone.

| Instruction | Relevant test exchanges | Preselected balanced model caught | Ordinary + context caught | New-only model caught |
| --- | ---: | ---: | ---: | ---: |
| Issues | 85 | 75 (88.2%) | 82 (96.5%) | 81 (95.3%) |
| Explicitly deferred work | 3 | 1 (33.3%) | 2 (66.7%) | 2 (66.7%) |
| Architectural decisions | 33 | 33 (100%) | 33 (100%) | 33 (100%) |
| External API constraints | 25 | 22 (88.0%) | 21 (84.0%) | 22 (88.0%) |

The deferred-work sample is especially small. These results do not establish population-level recall guarantees for any instruction.

## Context, unseen instructions and Sol replay

Before selection, 48 synthetic diagnostics were written: six cases for each original instruction and six for each of four unseen instructions—measured performance changes, accessibility barriers, credentials/personal information in logs, and saved-file compatibility commitments. They were not used for training or selection.

| Diagnostic | Preselected balanced + context | Ordinary + context | New exchange only |
| --- | ---: | ---: | ---: |
| Original instruction cases correct | 17/24 | 15/24 | 19/24 |
| Unseen instruction cases correct | 15/24 | 15/24 | 18/24 |
| Stale-context cases correctly skipped | 2/8 | 1/8 | 8/8 |
| Gate + cached Sol scenario outcomes correct | 15/16 | 15/16 | 16/16 |

All three models catch all 12 positive unseen-instruction cases. The unseen failures are unnecessary triggers: the selected model triggers on nine of twelve negative cases; the new-only model triggers on six. This small synthetic set does not demonstrate reliable generalization to arbitrary instructions.

Concrete failures:

- An old lost-update issue followed by an unrelated heading rename still triggers the context models. A cross-encoder does not automatically learn that only new evidence should trigger review.
- A privacy instruction triggers on an unrelated icon bug, and a compatibility instruction triggers on an unrelated SQLite decision. The gate sometimes treats the broader topic as sufficient.
- Both context models suppress the clear issue “The sync job drops every second update,” causing the failed new-folder reviewer scenario.
- Historical misses include a model-service capacity error and successful work that resolves a previous authentication or missing-contact blocker. Simply removing previous context can lose lifecycle updates.

**The Sol composition check reuses the previous 16 cached reviewer results. No new live Sol proposal-quality evaluation was run over the historical test set.** Historical Sol calls provide relevance labels, not final proposals or folder-routing judgments. The cached reviewer itself still passes 16/16; a gate can prevent it from seeing a necessary case.

## Data and labeling

The frozen sample contains 824 exchanges from disjoint connected-conversation groups:

| Partition | Exchanges | Groups |
| --- | ---: | ---: |
| Training | 472 | 142 |
| Development | 192 | 74 |
| Held-out test | 160 | 61 |

Development reuses the previous instruction experiment, including its already-inspected test data. The new test groups were selected from the remaining historical corpus before scoring or labeling. They are fresh for this instruction experiment, not newly collected production sessions. Missing sources were excluded before selection. Training has at most six exchanges per group; testing has at most four. Group separation, unique identifiers and frozen data hashes were verified.

Inputs recover complete user prompts and final assistant replies plus the previous exchange. Intermediate assistant commentary is not represented, although the runtime can include it.

Sol at high effort labeled each exchange independently for all four instructions. An initial agent audit inspected 20 training exchanges; a subsequent audit focused on deferred-work positives. Corrections removed unaccepted suggestions, routine task ordering and ordinary approval/access pauses from deferred work. There are 16 training-label corrections and ten development-label corrections, including two inherited from the previous experiment. Raw labels and reasons are preserved. This is agent adjudication, **not human ground truth**.

The still-sealed test sample was relabeled with the clarified policy. That choice was recorded before any test-label or prediction inspection. The two passes differ on 21/640 instruction judgments: ten issues, five deferrals, three architecture and three API labels. This is combined prompt/relabeling sensitivity, not independent annotator agreement. The primary labels have 88 relevant exchanges; the original labels have 84.

Against the original test labels, the selected model catches 80/84 (95.2%), versus 83/88 (94.3%) against the clarified labels. The instruction-specific failures remain under both label sets. No test labels or thresholds were changed after inspecting test errors.

## Models and training

Pretrained candidates were [MiniLM-L6 MS MARCO](https://huggingface.co/cross-encoder/ms-marco-MiniLM-L6-v2), [ModernBERT zero-shot](https://huggingface.co/MoritzLaurer/ModernBERT-base-zeroshot-v2.0), and [DeBERTa zero-shot](https://huggingface.co/MoritzLaurer/deberta-v3-base-zeroshot-v2.0-c). NLI models were tested with both the instruction/examples and short declarative descriptions. None avoided more than one of 192 development reviews at the recall criterion.

Four MiniLM training variants were compared, each with three checkpoints:

| Variant, final checkpoint | Development reviews skipped / 192 | Overall misses / 107 |
| --- | ---: | ---: |
| Ordinary training, previous context | 40 | 0 |
| Ordinary training, new exchange only | 38 | 0 |
| Balanced training, previous context | 48 | 1 |
| Balanced training, reinitialized output head, previous context | 15 | 0 |

Each run uses 1,888 instruction–exchange pairs, AdamW at `2e-5`, weight decay `0.01`, batches of eight pairs, three epochs and seed 42. The balanced loss gives positive and negative labels equal total weight within each instruction, using training counts only. An earlier run interrupted for label correction is archived and excluded from selection.

The joint input contains the instruction and approved examples (up to 160 tokens), 256-token windows of the new exchange with 64-token overlap, and optionally the last 64 tokens of the previous exchange marked as context rather than new evidence. Pairs fit within 512 tokens. Training applies binary cross-entropy to the maximum window logit; it does not falsely label every window of a positive exchange as positive. Inference uses the corresponding maximum score.

The preselected model is `balanced-context-epoch-3`, threshold **0.04057646915316582**, with 22.7M parameters. The base revision is `233902d25c440f23af6f7d6e94d2946bac0bee0a`. This comparison establishes improvement over the existing cosine implementation; it does not isolate cross-encoder architecture from task-specific fine-tuning, because the cosine baseline was not trained on the new pair labels.

## Runtime, cost and artifacts

On this Mac's CPU, the selected model scores the 160 test exchanges against four instructions in **12.94 seconds**. Repeating the first 16 exchanges individually gives **117 ms median / 206 ms p95** warm latency. This is a shared-machine measurement, excluding Python startup; it is not a production-worker benchmark. Inference makes no provider calls.

New labeling used 99 Sol calls, including the test relabel: **$6.45 estimated** under the repository's existing pricing convention, not a billing statement. The 192 development labels were reused.

Reproduction commands are in [README.md](README.md). Private artifacts are under `.local/instruction-cross-encoder/`: frozen samples, raw labels and corrections, call records, all trained weights, development scores, `selection.json`, `results.json`, `diagnostics.json`, labeling usage and artifact hashes. No live Den binary, instructions or gate installation was changed. No commits were created.

## Next iteration

Use the new-only model as a development baseline, while explicitly addressing history-dependent resolutions. Add training pairs that distinguish old evidence from new evidence, actual findings from hypothetical examples, accepted decisions from suggestions, and relevant findings from unrelated issues. Include substantially more instruction families and hold some families out entirely; three test deferrals and four synthetic unseen instructions are insufficient.

Keep direct review available for uncertain/reference-dependent cases. Evaluate the revised gate on fresh conversation groups and fresh user instructions before enabling it. The test sample in this report is now inspected and must not be reused as an untouched test after further tuning.
