# GLiClass fine-tuning and threshold evaluation — 2026-10-30

**The fine-tune became more selective, but did not establish a better gate at high recall. Do not enable these weights by default.** On unfamiliar instructions it skipped more reviews while doubling relevant misses at the selected operating point. Raising the threshold further increases that loss.

This is an actual local weight update, followed by frozen-threshold evaluation. It is a bounded experiment, not proof that GLiClass cannot be adapted successfully. No live Den model, instructions, or binary was installed, and no Sol proposal-review calls were made.

## Results at the selected development operating point

Both candidates were calibrated on the same 272 development exchanges, requiring at least 95% recall overall and for every instruction. That is a development selection rule, not a guarantee on evaluation data. The selected candidates both omit approved examples from gate input.

| Evaluation set | Gate | Reviews skipped, including misses | Relevant exchanges caught | Correct irrelevant skips |
| --- | --- | ---: | ---: | ---: |
| Reused historical, 160 exchanges | Pretrained | 7/160 (4.4%) | 87/88 (98.9%) | 6/72 |
| Reused historical, 160 exchanges | Fine-tuned | 15/160 (9.4%) | 86/88 (97.7%) | 13/72 |
| New synthetic instructions, 192 cases | Pretrained | 50/192 (26.0%) | 94/99 (94.9%) | 45/93 |
| New synthetic instructions, 192 cases | Fine-tuned | 81/192 (42.2%) | 89/99 (89.9%) | 71/93 |

The historical set activates the original four instructions together. Each synthetic case activates its designated instruction alone. Their skip fractions should not be compared as if they represent the same production workload.

The pretrained historical result differs slightly from the preceding report because this experiment recalibrated on an expanded development set. Its threshold is now 0.06453557685017586, versus 0.07303337752819061 previously. The scorer also batches up to eight passages. This round compares both models using the same scorer and calibration data.

### Individual instructions expose failures hidden by aggregate recall

| Historical instruction | Pretrained caught | Fine-tuned caught |
| --- | ---: | ---: |
| Issues | 84/85 | 82/85 |
| Deferred work | 3/3 | 3/3 |
| Architecture | 32/33 | 30/33 |
| External API constraints | 24/25 | 24/25 |

Fine-tuning reduces architecture recall to 90.9%. An exchange can still reach Sol through a different active instruction, so combined recall conceals some instruction-specific misses.

| Held-out instruction | Pretrained caught | Fine-tuned caught |
| --- | ---: | ---: |
| Missing provenance preventing reproduction | 6/6 | 6/6 |
| Release go/no-go conditions | 13/13 | 12/13 |
| Non-personal artifact retention commitments | 15/16 | 15/16 |
| Documentation contradicted by observed behavior | 13/13 | 12/13 |
| Handoffs without a responsible recipient | 10/13 | 8/13 |
| Contractual product-use limits | 13/14 | 13/14 |
| End-user workarounds for unmet needs | 12/12 | 11/12 |
| Units, time-zone, or currency assumptions | 12/12 | 12/12 |

The clearest regression is handoff ownership: the fine-tuned gate catches only 61.5% of positives. It misses an explicitly unclaimed alarm investigation after a shift handoff. It also misses a newly confirmed documentation discrepancy when the same message ends with unrelated lunch discussion, and a real whiteboard workaround for a missing dispatch status. These failures concern instruction interpretation and mixed evidence, not just selection of a convenient numerical cutoff.

## Does a higher passing threshold fix selectivity?

It makes the gate stricter, but useful findings are filtered too. These fixed cutoffs were specified before evaluation; none was chosen to rescue test errors.

| Fine-tuned score cutoff | Historical reviews skipped / 160 | Historical misses / 88 | New cases skipped / 192 | New misses / 99 |
| --- | ---: | ---: | ---: | ---: |
| 0.0484963, selected 95% development target | 15 | 2 | 81 | 10 |
| 0.10 | 17 | 2 | 83 | 11 |
| 0.25 | 27 | 4 | 87 | 12 |
| 0.50 | 43 | 12 | 91 | 15 |
| 0.75 | 62 | 23 | 103 | 22 |

For example, raising the selected cutoff to 0.50 skips 28 additional historical reviews, but ten of those additional skips are relevant. A score of 0.50 does not mean a calibrated 50% probability of relevance. Score scales also change after training, so equal numerical cutoffs across models are not equal operating points.

For a more conservative **99% development recall target**:

| Gate | Historical skipped / misses | New synthetic skipped / misses |
| --- | ---: | ---: |
| Pretrained, cutoff 0.03259064257144928 | 3 / 1 | 39 / 3 |
| Fine-tuned, cutoff 0.00029585712763946503 | 1 / 0 | 39 / 3 |

Both gates catch 96/99 new positives and correctly filter 36/93 new negatives at these conservative settings. Fine-tuning provides no improvement in those aggregate counts, while historical review avoidance falls to one exchange. Even this conservative setting misses retention/handoff cases; it is not a safety guarantee.

At the predeclared 90% development target, fine-tuning skips 86 new cases with 12 misses, versus 72 with 12 misses for pretrained. Thus it has learned useful separation at a lower-recall operating point, but that point loses too many findings for the intended default experience.

## Training and selection

Used the pinned `knowledgator/gliclass-v4-edge` revision `885af1af8a07edf4ed34df83ca0d995ed8201283` and the existing isolated environment. The first 20 encoder layers, embeddings, and other shared encoder parameters stayed frozen. The last four layers and classification/calibration components were updated: **56,261,636 trainable parameters out of 439,790,596**. No larger architecture or provider dependency was introduced.

Training had 867 available exchanges: 472 historical training exchanges, 156 unambiguous cases from the previous synthetic set, and 239 unambiguous newly authored cases. They form 2,283 labeled instruction/exchange pairs across 12 instruction types. Historical conversation groups remain separate from development and historical evaluation.

The run used two sampled epochs of 960 pairs, batch size eight, seed 42, AdamW at learning rate 0.00001, weight decay 0.01, and gradient clipping at 1. Sampling balances instruction/label buckets rather than following the historical bug-heavy class distribution. The 1,920 draws visited **748 distinct pairs**, so this bounded run did not train on every available pair. It took approximately 132 seconds on the M5 Max. Mean training loss fell from 0.484 to 0.329; that decrease is not held-out evidence.

Parameters were FP32 during training with BF16 Metal autocasting. Evaluation reloads weights in BF16. Inputs retain the prior 256-token passages, 64-token overlap, and up to 64 tokens of previous context. Long exchanges use hard passage selection: choose the strongest passage without gradients, then update on that passage. This approximates an exchange-level maximum without falsely assigning a positive exchange label to every passage. It can still reinforce a mistaken passage selection.

Native approved positive examples were included on half of training pairs. Each checkpoint and pretrained baseline was evaluated with examples present and absent, giving six development configurations. Selection used only the 192 historical development exchanges plus 80 cases from four additional calibration instructions: measured resource/cost changes, accessibility, personal-data exposure, and backward compatibility.

| Development configuration | Skipped / 272 | Combined misses / 147 |
| --- | ---: | ---: |
| Pretrained, no examples | 25 | 2 |
| Pretrained, examples | 13 | 0 |
| Epoch 1, no examples | 38 | 3 |
| Epoch 1, examples | 22 | 2 |
| Epoch 2, no examples | 48 | 4 |
| Epoch 2, examples | 31 | 1 |

All rows satisfy the per-instruction 95% development screen at their own calibrated threshold. The best trained candidate is epoch 2 without examples, cutoff **0.048496317118406296**. Examples did not improve high-recall filtering in this controlled comparison; this does not establish that examples are generally harmful. The feature's intended use of user-approved examples in the gate remains unresolved.

## Labeling, freezing, and limitations

Three smaller `gpt-6-luna` agents authored separate training, development, and held-out datasets, then independently audited a different agent's dataset without predictions. They produced 240/80/192 new cases. Each raw set includes previous context in half its cases. Only instruction text, optional approved examples, and conversation fields enter the model; labels, identifiers, explanations, and category fields are excluded.

The audits corrected two training labels and flagged one ambiguous training case, which was excluded. Four ambiguous cases from the previous synthetic set were also excluded from training. Development had no corrections and one ambiguity flag. Held-out auditing corrected 15 labels and flagged 11 ambiguous cases, resulting in 99 positives and 93 negatives. These are model-authored/model-audited labels, not human ground truth.

At the selected 95% development operating point, label sensitivity is:

| Label interpretation | Pretrained caught | Fine-tuned caught |
| --- | ---: | ---: |
| Audited primary labels | 94/99 | 89/99 |
| Original author labels | 93/96 | 88/96 |
| Excluding ambiguous cases | 90/94 | 85/94 |

The conclusion survives those alternatives. Some authored messages deliberately append unrelated small talk to relevant evidence; this is a useful challenge but an artificial distribution. The set includes non-coding work as well as product conversations. Each instruction has few positives, and the additional instructions share some broader reasoning patterns with training even though their exact rules and cases were withheld. This does not establish reliability for arbitrary future instructions.

Data hashes were frozen before training; models and thresholds were frozen before held-out scoring. The first held-out scoring attempt found a schema inconsistency: all 192 cases referenced instructions by their exact full text rather than identifier. The reader now normalizes those references. Source files, text, labels, checkpoints, and thresholds were unchanged, and the failure happened before any held-out scores completed. The original training script and the normalization record are preserved privately.

The historical comparison was already inspected in earlier experiments. The new synthetic set has now also been inspected and must not be reused as fresh held-out evidence for another tuning round.

## Local resource measurements

Both finalists were measured sequentially on the same 160 historical exchanges with four active instructions, on the **M5 Max / 128 GiB Mac**. Excluding the first exchange, both have approximately **106 ms median** and **680 ms p95** scoring time. The model's active tensor allocation is unchanged at about 0.88 GB; driver allocations were about 2.22 GB for both in this batched scorer.

Resident process memory after scoring was approximately 0.97 GB pretrained and 1.24 GB fine-tuned. Loading the FP32 fine-tuning archive reached a 2.63 GB resident peak before BF16 inference, compared with 0.97 GB for the pretrained archive. The saved checkpoints are research archives, not optimized deployment packages. Resident and GPU counters may overlap on unified memory and must not be added as exact physical usage. An ordinary 8–16 GB Mac was not validated.

## Recommendation and artifacts

Keep direct Sol review as the default. Preserve this checkpoint for research, but do not ship it as an accuracy improvement or raise its threshold by default. The experiment shows a selectivity/recall tradeoff, not a dependable solution for new user instructions.

The next useful experiment would target the observed boundary: mixed messages containing both a requested finding and unrelated discussion, and ownership/status evidence across coding and non-coding work. Include instruction-dependent contrasts: the same passage should pass under one rule and fail under another, with related variants kept in the same partition. Better evidence should precede another training sweep. Use new held-out instructions and realistically noisy exchanges, with a small human audit of the most consequential labels. A larger model or an API-only training set is not justified by these results.

- [Training, scoring, selection, and reporting script](finetune_gliclass.py).
- [512 new synthetic cases, original labels, corrections, and ambiguity flags](gliclass_finetuning_challenges.json).
- [Reproduction instructions](README.md#bounded-gliclass-fine-tuning).
- [Pinned environment](pretrained-requirements.lock.txt).
- Private frozen data, checkpoints, training history, scores, threshold comparisons, and audit records: `.local/gliclass-finetuning/`.

These results evaluate gate relevance only. They do not measure Sol proposal quality, folder routing, user acceptance, or production call savings after maintenance and fallback behavior.
