# Pretrained GLiClass and Qwen evaluation — 2026-10-30

**GLiClass is a plausible model to build on, but its pretrained gate is not yet worth enabling by default.** It skips only 8/160 historical reviews, including two relevant exchanges. It is more selective on newly authored instruction cases, skipping 35/160 with two misses, but its localization instruction catches only 6/8 positives. Qwen mostly lets everything through at the required recall. Keep direct Sol review as the default.

A bounded GLiClass fine-tuning experiment now has a concrete purpose: reduce unnecessary reviews while preserving instruction-specific recall and handling new findings versus old context. It should cover diverse instructions and hold out whole instruction families; training more API examples alone would not address the problem. No fine-tuning was performed in this round, and no live Den binary, gate, or instructions were changed.

## Historical comparison

These are the **same 160 historical exchanges used in the preceding experiment**, with its clarified labels. They were not used to calibrate this round's thresholds, but they were already inspected previously. They are a reused comparison set, not fresh held-out production evidence.

All four original instructions are active. Of the 160 exchanges, 88 warrant review under at least one instruction and 72 do not.

| Gate | Reviews skipped, including misses | Relevant exchanges allowed through | Recall |
| --- | ---: | ---: | ---: |
| Direct review | 0/160 | 88/88 | 100% |
| **Pretrained GLiClass v4 Edge** | **8/160 (5.0%)** | **86/88** | **97.7%** |
| Pretrained Qwen3-Reranker-0.6B | 1/160 (0.6%) | 88/88 | 100% |
| Previously fine-tuned MiniLM, new exchange only | 37/160 (23.1%) | 86/88 | 97.7% |

GLiClass correctly filters **6 of the 72 irrelevant exchanges** and still reviews 66. MiniLM correctly filters 35 and reviews 37. Qwen correctly filters one and reviews 71. Thus the skipped-review percentages above must not be read as entirely beneficial savings.

Conversation-group bootstrap 95% intervals for the skipped-review fraction are 1.9–8.6% for GLiClass and 15.2–31.8% for MiniLM. These describe this labeled sample, not production spending or arbitrary-instruction reliability.

### Each original instruction separately

The selected configurations score each instruction independently. The combined gate can conceal a miss when another instruction passes the exchange through.

| Instruction | Relevant exchanges | GLiClass caught | Qwen caught | Trained MiniLM caught |
| --- | ---: | ---: | ---: | ---: |
| Issues | 85 | 83 (97.6%) | 81 (95.3%) | 81 (95.3%) |
| Deferred work | 3 | 3 (100%) | 3 (100%) | 2 (66.7%) |
| Architecture | 33 | 32 (97.0%) | 31 (93.9%) | 33 (100%) |
| External API constraints | 25 | 24 (96.0%) | 24 (96.0%) | 22 (88.0%) |

GLiClass passes the earlier 95% recall screen on these four point estimates. The deferred-work sample has only three positives, so this is not a reliability guarantee. Its two completely suppressed exchanges mention a local Git-exclusion warning and API schema gaps/discrepancies in a documentation update. Labels were not revised after inspecting these errors.

The previous BGE cosine new-only result was 0/160 skipped with 88/88 allowed through. It was not rerun here; see [the preceding report](CROSS_ENCODER_RESULTS.md).

## Fresh instruction-generalization check

Added **160 authored exchanges**: 32 for the original four instructions and 128 for eight additional instruction families. The new families cover customer commitments, dependency licenses, localization mistakes, questions requiring an owner's answer, evidence contradicting hypotheses, monitoring gaps, approval requirements for destructive actions, and recurring workflow preferences.

The additional families were excluded from prompt and threshold development. Both new models were used pretrained; the MiniLM baseline was trained in the previous experiment. The public [challenge dataset](pretrained_gate_challenges.json) preserves cases, primary labels, original labels, and ambiguity flags.

There are 76 relevant cases under the audited primary labels and 84 irrelevant cases. Each case evaluates its designated instruction alone; these percentages are not directly comparable with activating all four instructions on historical conversations.

| Gate | Reviews skipped, including misses | Relevant cases allowed through | Recall |
| --- | ---: | ---: | ---: |
| **GLiClass** | **35/160 (21.9%)** | **74/76** | **97.4%** |
| Qwen | 1/160 (0.6%) | 76/76 | 100% |
| Trained MiniLM | 19/160 (11.9%) | 74/76 | 97.4% |

GLiClass correctly skips 33 irrelevant cases, versus 17 for MiniLM and one for Qwen. It still sends 51 irrelevant cases to review.

For the **eight additional instruction families only**:

| Gate | Reviews skipped / 128 | Relevant cases allowed through / 60 |
| --- | ---: | ---: |
| GLiClass | 30 | 58 |
| Qwen | 0 | 60 |
| Trained MiniLM | 13 | 59 |

GLiClass's two misses are both localization findings: a suspected Spanish-to-English fallback and loss of a character during Korean text entry. That instruction catches **6/8 positives**, despite the good aggregate recall. MiniLM misses an explicit queue-to-direct-calls architectural decision and a recurring preference about planning discussions. Both models still trigger on routine passing tests and feature requests without a reported defect.

### Labeling and limitations

One `gpt-6-luna` agent authored and labeled the cases; a second independently audited all 160 without predictions. The audit corrected four labels involving whether a named department or role satisfies an instruction requiring a **named person**. Those cases remain marked ambiguous. The parent agent reviewed 26 cases, including all four ambiguity flags, after thresholds were frozen and before predictions; it made no further corrections.

The original labels contain 80 positives. Against those labels, GLiClass catches **76/80 (95.0%)**, Qwen 80/80, and MiniLM 78/80. Excluding the four ambiguous cases, GLiClass skips 33/156 and catches 74/76. This sensitivity is retained rather than hidden.

These are model-authored and model-audited labels, **not human ground truth**. The authored exchanges are short and often clearer than real conversations; some assistant replies explicitly state the interpretation or note-taking outcome. Only two fresh cases contain an explicit previous exchange. Therefore this set does not establish reliable long-conversation or lifecycle behavior. The old corpus had no recoverable, unused conversation groups left; the newly available Den sessions concerned the experiment itself and were not substituted as independent production data.

The fresh dataset is now inspected. Future tuning can use it as development material, but a subsequent claim of held-out performance needs a new evaluation set.

## Existing lifecycle diagnostics

The frozen configurations were also evaluated on the previously authored 48-case diagnostic set. These cases were already inspected in the earlier experiment and are **not fresh validation**.

| Gate | Relevant cases caught / 24 | Correct irrelevant skips / 24 | Stale-context cases correctly skipped / 8 |
| --- | ---: | ---: | ---: |
| GLiClass | 23 | 14 | 6 |
| Qwen | 24 | 1 | 1 |
| Trained MiniLM | 24 | 13 | 8 |

GLiClass still triggers on an unrelated heading change following an old issue or API limit. It also narrowly suppresses an explicit decision to separate a parser from the UI: score 0.072900 versus threshold 0.073033. The threshold was not changed to rescue this inspected case. These failures make freshness and decision boundaries concrete subjects for a possible follow-up, rather than reasons to claim generalization is solved.

## Configurations and selection

Used the existing **192-exchange development set** and its corrected labels. For each candidate, chose a single shared threshold that maximized reviews avoided while retaining at least 95% recall overall and for each instruction. No per-user thresholds or model-weight updates were learned. Configurations and thresholds were frozen before fresh predictions.

| Development configuration | Skipped / 192 | Combined misses / 107 |
| --- | ---: | ---: |
| GLiClass, binary labels with full policy and native positive examples, new exchange only | 5 | 0 |
| GLiClass, instructions as joint labels, no examples | 0 | 0 |
| GLiClass, simpler binary prompt, minimal previous context, no examples | 10 | 2 |
| Qwen, full gate policy and examples, new exchange only | 0 | 0 |
| Qwen, simpler prompt, minimal previous context, no examples | 0 | 0 |
| Trained MiniLM baseline | 38 | 0 |

The joint-label experiment's per-label statistics condition on all four labels being supplied together; they are not standalone-instruction guarantees. It was not selected. The final GLiClass gate checks instructions separately, despite the model's support for scoring multiple labels together.

The best GLiClass format uses the user's instruction directly with two descriptive matching labels, complete new evidence in 256-token passages with 64-token overlap, and up to 64 tokens from the previous exchange marked as context rather than new evidence. It does not use the approved examples. The richer example-based format was tested, but prompt, examples, and context changed together, so this is not an isolated demonstration that examples hurt. Making user-approved examples effective remains unfinished work before runtime integration.

Qwen's selected format uses its documented `Instruct` / `Query` / `Document` template and yes/no logits. It receives the full gate policy, instruction, and approved examples, with new evidence only. The simpler contextual format tied at zero savings, so the original new-only format was retained. The scorer reproduced the publisher-style Beijing retrieval example, including identical tokenization between the assembled prompt and separately encoded template sections.

CPU FP32 selected the formats. Their BF16/Metal versions were recalibrated on the same development set before fresh testing; they are the two pretrained configurations in the result tables. This avoids assuming reduced precision preserves every decision. In an early four-exchange check, GLiClass's maximum score difference between CPU FP32 and Metal BF16 was about 0.012.

Final thresholds:

- GLiClass BF16/Metal, simple prompt with context: **0.07303337752819061**.
- Qwen BF16/Metal, original new-only format: **0.000021785308490507305**.
- Previous trained MiniLM, CPU FP32/new-only: **0.005811875686049461**.

Lower recall targets do not reveal a dramatic pretrained win. At a 90% development recall requirement for every instruction, GLiClass skips 18/192 with seven combined misses; Qwen skips 6/192 with two misses. At 99%, GLiClass skips 2/192 and Qwen zero. These are development tradeoffs, not retuned test results.

## Local runtime and memory

Measured on the development **M5 Max / 128 GiB Mac**, not an ordinary 8 GB machine. The following benchmarks ran one model at a time over the same 16 development exchanges, with four instructions and four CPU threads configured. GPU runs use Apple Metal. The sample includes longer exchanges and is too small to characterize all production tail latency.

| Configuration | Warm median | Warm p95 | Loader start through first review | Process resident memory | GPU-driver allocations |
| --- | ---: | ---: | ---: | ---: | ---: |
| GLiClass BF16/Metal | 100 ms | 830 ms | 3.47 s | 0.81 GB | 1.15 GB |
| Qwen BF16/Metal | 85 ms | 535 ms | 2.77 s | 0.72 GB | 2.34 GB |
| Trained MiniLM FP32/CPU | 27 ms | 191 ms | 2.02 s | 0.76 GB | — |
| GLiClass FP32/CPU | 733 ms | 5,529 ms | 7.85 s | 3.45 GB | — |

Memory uses decimal GB. Resident memory and GPU-driver allocations are different counters and may overlap on unified-memory hardware; they are not an exact additive measure of physical memory. GLiClass's process resident memory rose to about 0.99 GB during the complete development run, with roughly 1.15 GB of driver allocations. Qwen's driver allocation dropped from 2.34 GB to 1.22 GB after releasing unused cache while retaining model weights.

On one short exchange, GLiClass took approximately 20 / 62 / 198 ms for 1 / 4 / 12 instructions. This demonstrates scaling in this implementation, not performance on a weaker Mac.

The Apple GPU was unavailable inside the execution sandbox and available outside it. GLiClass's pipeline also silently maps the string `"mps"` to CPU; passing `torch.device("mps")` and verifying the model's actual device fixes that. The mistaken CPU-fallback smoke run was archived and excluded.

CPU BF16 reduced memory to about 1.34 GB in a four-exchange check but was far slower. A simple QNNPACK INT8 conversion was also slower and materially shifted scores; its calibration module must remain unquantized because it reads linear-layer weight dtype directly. Neither CPU reduced-precision experiment is a selected gate or a validated deployment format. GGUF/ONNX deployment optimization was not undertaken.

The isolated Python environment occupies about 1.1 GB on disk; both downloaded model snapshots together occupy about 2.0 GB. All gate inference is local. Labeling used the requested smaller-model agents; no new Sol proposal-review calls were made.

## Recommendation

Keep GLiClass as the pretrained research candidate and stop prioritizing Qwen for this gate. GPU speed is plausible, and GLiClass's original-instruction recall is encouraging, but 5% historical review avoidance does not justify enabling this worker by default. Neither its fresh instruction-family results nor its stale-context behavior establish the experience we want.

If we continue, run one bounded fine-tuning experiment aimed at **actual requested evidence versus merely related discussion**, including status changes and unrelated new messages after an old finding. Mix many instruction families, keep related rewrites together, include counterexamples, and reserve entire families for evaluation. The objective is to approach MiniLM's historical savings without losing GLiClass's instruction-specific recall, not to increase an API-only score. Check approved-example behavior explicitly. Prepare new held-out material before tuning; the results in this document must not become a repeatedly optimized test.

These experiments evaluate the gate's relevance decisions. They do not evaluate new Sol proposal quality, folder routing, or production cost after batching and maintenance fallbacks. Pending-card maintenance can recover some lifecycle cases and reduce achievable savings. No live feature installation or commit was made.

## Artifacts

- [Scoring, calibration, reporting, and benchmarking script](pretrained_gate.py).
- [Pinned pretrained-model environment](pretrained-requirements.lock.txt).
- [New authored evaluation cases and labels](pretrained_gate_challenges.json).
- [Model research and source links](PRETRAINED_MODEL_RESEARCH.md).
- Private artifacts: `.local/pretrained-instruction-gate/`, including source snapshots, all scores, frozen selection, label audits, sensitivity results, resource measurements, and excluded smoke runs.
- Exact pretrained revisions are pinned in `pretrained_gate.py`; the baseline is the previous `trained-new-epoch-3` checkpoint. Reproduction commands are in [README.md](README.md).
