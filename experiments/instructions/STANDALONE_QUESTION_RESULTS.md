# Decider 2B with standalone questions — 2026-11-01

**Converting instructions into concrete questions did not make this Decider configuration a useful high-recall gate.** At the threshold selected on development data, it skipped no historical exchanges and only three unfamiliar-instruction cases. At the ordinary 0.5 cutoff it did substantially better on the authored unfamiliar cases, but missed most relevant historical exchanges.

This run used the installed pretrained Mapika Decider 2B v12 Q4_K_M model on Metal. It changed the questions only; weights, labels, passage formatting, runtime, and the threshold-selection rule match the previous Decider compact experiment. It evaluates the gate, without Sol proposal generation, product integration, or user-facing instruction conversion.

## Questions

All 16 benchmark instructions were manually converted from their instruction text before any scoring. The entire [question mapping](standalone_gate_questions.json), including the eight unfamiliar instructions, was frozen in the development artifact and threshold selection. The scorer rejects question changes when resuming or evaluating comparison sets. Questions were not rewritten after inspecting scores.

The four original instructions became:

| Instruction | Exact question |
| --- | --- |
| Issues and their status | Does the new exchange describe a problem, bug, failure, or unexpected behavior, or report progress, a fix, or evidence that rules out a previously suspected problem? |
| Deferred work | Does the new exchange describe work deliberately left for later, or report a change to or completion of previously postponed work? |
| Architectural decisions | Does the new exchange state or revise a decision about software architecture, technology choices, or design, or explain why such a decision was made? |
| External API constraints | Does the new exchange describe a restriction or required behavior of an external API, a workaround for it, or a change to a previously known restriction or workaround? |

Decider receives a passage and one question at a time through its native Choice interface, with options `no` and `yes`. Questions contain no references to Den, Sol, proposals, or whether a review is warranted. The original instruction and approved examples are not included in the model input. Previous context, when present, remains separately marked as reference only.

This experiment tests concrete instruction-specific predicates. It does not test a generic topical-similarity question, automatically generated questions, or examples in the gate input.

## Results with the frozen development threshold

The selection rule maximizes skips while maintaining at least 95% relevant-exchange recall overall and for every development instruction. The resulting threshold was **0.03780808858573437**, frozen before scoring the historical and unfamiliar sets. This is a selection criterion, not a guarantee of future recall.

Each cell below is **skipped exchanges / total exchanges; missed relevant exchanges / relevant exchanges**. Skips include misses.

| Gate | Development | Historical sessions | Unfamiliar instructions |
| --- | ---: | ---: | ---: |
| Decider, standalone questions | 2/272; 0/147 | 0/160; 0/88 | 3/192; 0/99 |
| Decider, earlier review question | 1/272; 0/147 | 0/160; 0/88 | 0/192; 0/99 |
| Trained GLiClass | 48/272; 4/147 | 15/160; 2/88 | 81/192; 10/99 |
| Pretrained GLiClass | 25/272; 2/147 | 7/160; 1/88 | 50/192; 5/99 |

Baseline scores were reused and their metrics recomputed against the same frozen data. No baseline inference or training was repeated. The GLiClass rows use their own previously frozen thresholds selected by the same rule; their extra skips come with relevant misses.

The standalone gate catches every relevant exchange at its selected threshold largely because it passes almost everything: all 72 irrelevant historical exchanges and 90 of 93 irrelevant unfamiliar cases still reach Sol. The three unfamiliar skips are all correct, for **1.6%** review avoidance in that set.

### Individual instructions

The historical set activates four instructions together. A positive result for any instruction sends the exchange onward, which can hide a miss for another instruction.

| Historical instruction | Relevant cases caught | Irrelevant cases passed |
| --- | ---: | ---: |
| Issues | 85/85 | 73/75 |
| Deferred work | 3/3 | 157/157 |
| Architecture | 31/33 | 71/127 |
| External API constraints | 25/25 | 133/135 |

Architecture also misses two of 44 relevant development pairs. Those exchanges still pass through other instructions. All eight unfamiliar instruction families catch every labeled positive at the selected threshold; each unfamiliar case activates just its own instruction.

## Ordinary yes/no decisions at 0.5

The fixed 0.5 cutoff shows why the conservative threshold was necessary. It was inspected as a diagnostic, not selected using comparison labels.

| Set | Earlier question: caught relevant / correct irrelevant skips | Standalone questions: caught relevant / correct irrelevant skips |
| --- | ---: | ---: |
| Development | 66/147; 93/125 | 56/147; 112/125 |
| Historical | 28/88; 57/72 | 21/88; 66/72 |
| Unfamiliar instructions | 85/99; 50/93 | 89/99; 77/93 |

The standalone questions improve both relevant detection and irrelevant filtering on the short authored unfamiliar cases: **89.9% recall** and **86.5% combined accuracy**. However, historical recall is only **23.9%**, down from 31.8% with the earlier question. The datasets differ in instruction families, conversation style, length, and dependence on context, so this comparison does not isolate which difference causes the gap.

For example, an authored development exchange reports a signed download URL appearing in public job output, two anonymous reads, and disabling the signing key. The exposure question scores it only 0.2463. The low threshold lets it through; the ordinary yes/no cutoff misses it. Errors therefore remain even with a concrete question and a fairly direct passage.

At 0.5, unfamiliar-family misses are one provenance case, two release cases, two retention cases, and five unit/time/currency cases. The remaining four unfamiliar families have no relevant misses, which is useful evidence for bounded tasks but not a reliable general gate.

## Memory and time

Runs were sequential on the same M5 Max with 128 GiB unified memory as the previous experiment, with four CPU threads, Metal inference, and a 1,024-token model context.

| Set | Peak process resident memory | Median scoring per exchange | 95th percentile |
| --- | ---: | ---: | ---: |
| Development | 2.302 GB | 136 ms | 1.271 s |
| Historical | 2.298 GB | 145 ms | 856 ms |
| Unfamiliar | 2.298 GB | 22 ms | 31 ms |

Model loading took 1.78–1.87 seconds. Times include all passage/question scoring for an exchange, but exclude input tokenization and model loading. The historical set has four active questions; unfamiliar cases have one. Total native scoring across all sets was about 123 seconds.

Resident memory is not total system memory pressure. Metal reservations can overlap resident memory and must not be added to it as independent allocations. An actual 8–16 GB laptop was not tested; these results do not establish performance or headroom on that hardware.

## Reproduction and limits

The [scorer](system_one_evaluate.py) preserves the earlier layout: 256-token passages, 64-token overlap, up to 64 tokens of separately marked previous context, and the maximum yes score across passages. Model context is 1,024 tokens and passage input budget is 768. Each question is scored separately with native Choice probabilities.

Run these commands in order using the installed local environment. Metal access is required in this setup; selection refuses to overwrite an existing frozen threshold.

```sh
.local/system-one-models/venv/bin/python experiments/instructions/system_one_evaluate.py score --model decider-questions --split development
.local/system-one-models/venv/bin/python experiments/instructions/system_one_evaluate.py select --model decider-questions
.local/system-one-models/venv/bin/python experiments/instructions/system_one_evaluate.py score --model decider-questions --split historical
.local/system-one-models/venv/bin/python experiments/instructions/system_one_evaluate.py score --model decider-questions --split heldout
.local/system-one-models/venv/bin/python experiments/instructions/system_one_evaluate.py report --model decider-questions
```

All 624 exchanges and 1,680 instruction/exchange pairs completed. Dataset hashes, complete identifier coverage, instruction coverage, the frozen question mapping, and the development selection fingerprint were verified. Per-case probabilities, aggregate comparisons, exact questions, threshold, timing, and memory records are under `.local/system-one-evaluation/decider-questions/`.

These are reused comparison sets, including previously inspected, model-authored and audited unfamiliar cases. No new labels were created or changed. Their labels describe whether the original instruction calls for attention, rather than generic topical relatedness. Manual question conversion may alter an instruction's boundary, and the deployed model is a Q4 configuration; this result does not establish that all question formulations, precisions, or decision models fail.

The evidence does not justify enabling this Decider configuration or building the proposed user workflow around it yet. The clearer question format remains a reasonable experimental interface. Its stronger results on the authored cases are a reason to investigate the gap with real exchanges, not evidence that it is ready for arbitrary user instructions.
