# Local gate comparison — 2026-10-21

Task-specific fine-tuning improved message-level accuracy substantially. **ModernBERT with SetFit**, selected using validation accuracy, scored **80.4% on the held-out test**, versus **62.2% for the current Luna gate**. The improvement was **18.3 percentage points**, with a conversation-group bootstrap 95% interval of **+9.4 to +27.1 points**.

**BGE small with SetFit had the highest observed test score, 82.4%.** Its validation score was lower, so it was not the preselected model. Its 1.9-point test advantage over ModernBERT is inconclusive: the paired interval is **−3.3 to +7.0 points**. Keep ModernBERT as the validation-selected candidate and BGE as a much smaller challenger. This experiment supports SetFit; it does not establish a universal winner between those two encoders.

## Data and procedure

- Read 740 local Claude/Codex transcript files, including archived Codex sessions. After filtering helper runs, injected instructions, missing replies, duplicates, and this experiment's conversation: **1,843 exchanges from 295 sessions**, spanning 30 working-directory paths.
- Connected forks and duplicated conversations before splitting: **1,266 training examples / 193 groups**, **265 validation / 46 groups**, **312 test / 43 groups**. No group crosses a split. The test set contains 170 positive and 142 negative labels.
- Labeled all examples with **gpt-6.1-sol, high reasoning effort**, without showing it the current gate's answers. The target was durable information warranting a careful review, not predicted card acceptance.
- A blind **gpt-6-astra** audit agreed on **62/64 labels (96.9%)**. Against that second model's labels, the selected classifier scored 57/64 and the current gate 35/64. These are model judgments, not human ground truth.
- Trained each logistic head on training examples only. Selected regularization, routing neighbors, thresholds, and the final method using validation accuracy, with macro F1 breaking ties. SetFit trained each encoder for one epoch over 5,064 contrastive pairs, at a learning rate of `1e-5`.
- Replayed the current gate's actual system instructions with **gpt-6-luna, no reasoning**, once per held-out example. The surrounding local rules and scheduling were not included in the primary comparison.

## Results

False positives are unnecessary reviews; false negatives are missed reviews. All test columns use the same 312 examples. Routing means cosine matching against labeled training examples, implemented directly rather than through the Semantic Router package.

| Method | Validation accuracy | Test accuracy | False positives | False negatives |
| --- | ---: | ---: | ---: | ---: |
| Current Luna gate | — | 62.2% | 22 | 96 |
| Training-majority baseline: skip | — | 45.5% | 0 | 170 |
| Frozen BGE small + logistic head | 78.1% | 78.2% | 52 | 16 |
| Frozen ModernBERT + logistic head | 80.4% | 76.0% | 33 | 42 |
| Frozen Qwen3 0.6B + logistic head | 80.8% | 73.4% | 27 | 56 |
| Frozen BGE small + routing | 75.1% | 76.3% | 60 | 14 |
| Frozen ModernBERT + routing | 75.8% | 75.0% | 72 | 6 |
| Frozen Qwen3 0.6B + routing | 76.6% | 76.6% | 60 | 13 |
| SetFit BGE small + logistic head | 80.4% | **82.4%** | 39 | 16 |
| **SetFit ModernBERT + logistic head — selected** | **84.5%** | **80.4%** | **32** | **29** |
| SetFit BGE small + routing | 81.1% | 76.3% | 35 | 39 |
| SetFit ModernBERT + routing | 83.4% | 80.8% | 42 | 18 |
| ModernBERT NLI, zero-shot | 53.2% | 47.4% | 4 | 160 |
| ModernBERT NLI, calibrated threshold | 63.4% | 53.8% | 24 | 120 |
| DeBERTa NLI, zero-shot | 59.6% | 51.3% | 11 | 141 |
| DeBERTa NLI, calibrated threshold | 61.1% | 54.5% | 33 | 109 |

The selected classifier's precision was **81.5%** and recall **82.9%**, versus **77.1%** and **43.5%** for the current gate. It caught 67 more worthwhile exchanges at the cost of 10 more unnecessary reviews. That is 57 additional correct decisions overall.

The larger frozen Qwen encoder did not earn its extra size here. NLI performed poorly with these three fixed descriptions, even after threshold calibration. SetFit and routing are composable: fine-tuned embeddings helped routing, but the selected final method uses a logistic head.

## Controls and remaining errors

Replacing the missing historical folder marker with the neutral name `Work` preserved the result: **80.8% for the selected classifier versus 63.8% for the gate**, with an improvement interval of **+8.4 to +25.4 points**. The missing folder name does not explain the main improvement.

Reloading the saved model and recomputing all 312 test predictions changed **zero decisions**. A separate sanity check on 13 original logged gate prompts scored **10/13 for the classifier versus 8/13 for the recorded gate answers**. Those logs include buffered exchanges and overlap historical sessions, so they are not an additional independent test set.

The error review found meaningful boundaries still to learn:

- Confirmed constraints can look like routine “added this to the notes” reports. The selected model missed an exchange recording constraints needed for future tool-call assertions (`30e45d17507ce71e5da8`).
- A summary of concluded decisions can end in a question and still deserve retention. One such interface decision summary was missed (`3ae721226fdaac178418`).
- A generic tool-schema answer can resemble a durable project finding. One was incorrectly flagged (`047ba304d53cd92491a5`).
- Unconfirmed proposals can resemble accepted decisions. A proposed set of changes awaiting approval was incorrectly flagged (`1eea6421e0de98b0f677`).
- The classifier recovered causal explanations the gate missed, including why a UI overlay uses a separate portal (`044ff0971ec1012ca913`).

Private `errors.jsonl`, labels, and call records retain the evidence for these identifiers. One apparent false positive was also one of the two label-audit disagreements; the boundary between operational trivia and useful constraints is not perfectly settled.

## Runtime and artifacts

Approximate batch embedding times on this Mac, excluding model loading: BGE small **12.7 ms/exchange on CPU**, ModernBERT **53.0 ms on CPU**, Qwen3 0.6B **300.9 ms on CPU**. After fine-tuning, BGE measured **12.6 ms on CPU** and ModernBERT **11.4 ms on Metal GPU**. These are experiment timings on a shared machine, not controlled production latency measurements. The prediction CLI also pays Python startup and model-loading time.

Both trained encoders, their classification heads, thresholds, routing exemplars, label files, model-call records, model revisions, environment versions, and aggregate results were saved outside Git. The selected configuration is `modernbert-setfit`, logistic regularization parameter `C=10`, decision threshold `0.625`. The BGE alternative uses `C=10`, threshold `0.475`.

The repository contains the reproducible [experiment scripts and instructions](README.md). This report describes the original offline experiment. On 2026-10-23 the user chose to deploy the selected classifier immediately; see the [production integration and artifact handoff](../../docs/classifier-gate.md).

## What this establishes

This is evidence that a locally fine-tuned encoder can improve this user's message-level gate decisions. It is not yet a complete production replacement evaluation. Inputs preserve a 700-character prompt prefix and reply suffix, approximate historical goals, and no historical waiting-card state. The production gate can buffer multiple exchanges and consider waiting cards; withdrawal and deduplication were not adequately represented. There were no positive folder or withdrawal labels; positives were predominantly notes.

The next evaluation should use fresh, logged production inputs, including pending-card corrections and buffered exchanges. The initial suggestion was shadow mode; the user subsequently chose immediate deployment with detailed logging. Keep the held-out examples out of further training if this test set is to remain useful.
