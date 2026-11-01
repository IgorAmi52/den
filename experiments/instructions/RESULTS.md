# User instructions: implementation and evaluation

Follow-up: [cross-encoder training and fresh held-out evaluation](CROSS_ENCODER_RESULTS.md). Fine-tuning reduced review calls, but instruction-specific recall and stale-context failures still prevent recommending it as the default gate.

Evaluated on branch `feat/user-instructions`, 2026-10-28. The instruction, chat, routing and proposal flow is implemented. **Use direct review by default. The cosine gate is functional but remains experimental: its conservative setting did not save calls with all four behaviors enabled.** No installed Den binary, live instructions or existing classifier was replaced.

## Delivered behavior

- One Instructions row using the existing panel/editor; chat drafts illustrative examples, supports revisions before approval, and saves/updates/pauses/resumes/deletes instructions through confirmation cards.
- Readable private Markdown with stable identifiers, repo/folder observation scope, status, behavior and approved examples. Folder scope includes descendants. Behavior changes invalidate old example approval; stale writes fail visibly.
- Instructions govern Den only. No historical retrieval for instruction setup; the instruction panel supplies no session transcripts. Instructions never enter launched coding-session context.
- One replacement reviewer policy: saved instructions define relevance. Removed mandatory future-use justification, three-line truncation and the old automatic plan/link/skill proposal paths.
- Compact folder index with cached deterministic excerpts and explicit purpose; targeted full destination/ancestor reads. No folder taxonomy or folder per instruction. New instruction-based folders do not move source sessions.
- Evidence quotes, instruction revisions and destination snapshot versions are validated. Existing context can be corrected or removed without discarding unrelated content. Existing proposal discussions, rejection feedback and backlog revalidation remain.
- Optional provider-independent local cosine matching over complete messages; a persistent worker caches approved example vectors. Missing examples, absent runtime, pending-card maintenance and inference failures allow direct review. The legacy classifier is not an upstream veto.

## Historical relevance evaluation

Selected 192 exchanges from the existing corpus before scoring models: 96 validation and 96 held-out, across 74 connected conversation groups. Reused the original grouped validation/test assignments, with a deterministic sampling seed and at most four exchanges per session. No original training exchanges were selected; the existing fine-tuned encoder had not trained on these groups.

Recovered full user prompts and final assistant replies for all 192 exchanges, including the preceding exchange for reference interpretation. These are full final replies, not the old 700-character excerpts. Intermediate assistant commentary is not represented in this historical sample; the runtime additionally retains those messages. This coverage difference remains a limitation.

Relabeled all exchanges for the four frozen instructions in [instructions.json](instructions.json): issues, deferred work, architectural decisions and external API constraints/workarounds. Sol at high reasoning effort labeled each instruction separately, distinguishing review relevance from a need to issue a proposal. The implementation agent inspected 20 validation exchanges, including positives and skips, and corrected two labels: an unaccepted deferral recommendation and a description of existing behavior that did not establish an architectural decision. This is model-assisted evaluation, not independently adjudicated human ground truth.

Examples and sample were frozen before model comparisons. Model selection used validation only. An initial union-gate criterion was tightened **before opening held-out results** to require at least 95% recall when each instruction is enabled alone; otherwise one broad instruction can conceal another's misses. No held-out threshold retuning was performed.

Compared BGE-small, the existing ModernBERT SetFit encoder, and Qwen3 Embedding 0.6B. Also tried BGE with the instruction itself as an additional anchor and without preceding context; that variant did not improve high-recall savings. All candidates used normalized cosine scores and positive examples, without retraining.

| Encoder | Validation threshold meeting the stricter criterion | Reviews avoided with all four instructions |
| --- | ---: | ---: |
| BGE small | 0.51 | 0 / 96 |
| Existing ModernBERT SetFit | 0.255 | 0 / 96 |
| Qwen3 Embedding 0.6B | 0.26 | 0 / 96 |
| BGE plus behavior anchor, no previous context | 0.435 | 0 / 96 |

BGE wins the tie on size and latency. Its held-out result at 0.51:

| Enabled behavior | Relevant exchanges caught | Reviews avoided |
| --- | ---: | ---: |
| Issues alone | 42 / 42 | 3 / 96 |
| Deferred work alone | 8 / 8 | 14 / 96 |
| Architecture alone | 28 / 28 | 11 / 96 |
| API constraints alone | 15 / 15 | 1 / 96 |
| All four | 52 / 52 distinct exchanges | **0 / 96** |

The all-four recall is trivial because every exchange passed. It is not evidence of a useful classifier. Sampled three-exchange grouping also avoided zero batches. Fallbacks can only reduce savings further.

Increasing the threshold causes material misses. On validation, BGE at 0.60 avoided 22 of 96 reviews but missed 10 of 56 relevant exchanges; at 0.65 it avoided 75 reviews but missed 41 relevant exchanges. These tradeoffs are unsuitable for a behavior such as “catch every issue.” Three illustrative examples are not a reliable general relevance boundary for arbitrary user instructions. A larger embedding model did not solve that in this experiment.

## Reviewer and interaction evaluation

Ran the actual Go `ReviewRequest`, authenticated Sol helper, proposal validator and store against 16 isolated synthetic scenarios in [scenarios.json](scenarios.json). The final run passed **16/16**:

- Top-level and nested issue routing, reusable new-folder proposals, uncertainty, duplicate suppression, resolution and disproof.
- Explicit deferred work and its completion, with routine next steps skipped.
- Confirmed architectural decisions versus unaccepted suggestions.
- External API caps, internal UI limits, superseded workarounds and generic API statements.
- Transcript instruction injection rejected as evidence of user intent.

Assertions check destination, proposal presence, exact evidence, lifecycle replacement, preservation of unrelated context and no automatic source-session movement. The first pass failed one case: the model proposed an individual issue folder whose name exceeded the existing limit. Tightening the policy to short reusable workspace names fixed it; the full suite was rerun. This development suite was iterated on, so 16/16 is a regression result, not held-out proposal accuracy.

Replayed those same 16 cases through the installed local gate, with each scenario's instruction enabled. All were sent to review: **no missed proposals and no reviewer savings** compared with direct Sol. Reusing the direct reviewer answers isolates gate decisions without paying for identical model calls twice.

The five-step real chat flow passed after a final replay exposed an invalid draft that embedded examples in the behavior text. The output contract now separates those fields explicitly, draft history preserves their structure, and invalid actions show a clear validation message instead of an unusable approval question. The flow covered: draft without saving, revise an unsaved draft's scope/examples, save the accepted revision, pause, resume and delete. Twenty-four local checks covered Markdown roundtrip, scope inheritance, stale writes, changed examples, exclusion from coding context, fallback behavior, evidence/context validation and preservation of more than three findings.

An isolated tmux UI smoke check covered the Instructions row, opening its panel, manual editing, saving canonical Markdown and reading the result at 120×40 and 80×30. This exposed and corrected misleading context-specific headings. The live user's tmux session was not used. External-editor launch was not exercised interactively.

## Cost and latency

Historical labeling used 24 Sol calls. Recorded token usage: 286,484 input tokens (286,412 reported as cache writes), 61,283 output tokens and 35,557 reasoning-output tokens. Using the repository's pricing convention, estimated labeling cost was **$1.68**. This is a token-based estimate, not a billing statement.

The final 16-case reviewer run cost **$0.395 estimated**, about **2.47 cents per review**, and took 86.6 seconds summed across calls. Development reruns and chat checks add to this; raw usage is preserved with each local call. There are no provider calls in local matching.

BGE CPU inference over the historical sample took 6.8 seconds; sampled warm single-exchange median was 13 ms, with a 99 ms p95. Model comparisons were not a controlled hardware benchmark. The Go runtime check measured **2.94 seconds cold**, **6 ms warm**, and **124 ms** for a long message with a relevant finding buried beyond the old truncation boundary. The worker's corrupt-artifact check correctly allowed direct review and logged the failure. These are measurements on this machine, not latency guarantees.

## Reproduction and artifacts

Public scripts, synthetic scenarios and instructions are in this directory. Private data stays in ignored paths:

- `.local/instruction-evaluation/`: frozen sample, manifest, full prompts, labels, label corrections, candidate scores, selection history, held-out results and original model-call records.
- `.local/instruction-replay/`: synthetic requests/answers, validation outcomes, usage, chat checks and runtime checks.
- `.local/instruction-runtime/`: isolated experimental BGE install.
- `.local/instruction-ui/`: isolated UI state and terminal captures.
- `.local/den-instructions`: built branch binary.

The sample manifest and selection files preserve the pre-scoring sample fingerprint and pre-test model choice. No real historical passages or user datasets are included in source control. See [README.md](README.md) for commands.

The practical recommendation is to try the instruction-driven experience using direct review and collect accepted/rejected proposals with scoped feedback. Cost reduction needs a better relevance mechanism or stronger example coverage, measured on a fresh held-out set. Do not present the current cosine gate as a proven cost optimization.
