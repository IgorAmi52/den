# Local decision-model gate evaluation — 2026-11-01

**These initial runs used the stronger “warrants review” question, rather than the user's intended isolated detection questions.** Their results below describe that earlier formulation. See the [standalone-question follow-up](STANDALONE_QUESTION_RESULTS.md) for the corrected Decider 2B experiment: 0/160 historical and 3/192 unfamiliar exchanges skipped at its frozen development threshold, with no combined relevant misses. The relation-question variants were added to the scorer but were not evaluated.

In the initial runs below, at the development threshold selected to retain at least 95% recall overall and for every instruction, Jev-Style 0.8B skipped no historical or unfamiliar-instruction reviews; Decider 2B also skipped none. Both ran locally. These configurations did not establish a useful default gate for Den.

This evaluation compares gate decisions, not Sol's later proposal reasoning or folder placement. No live Den gate or installed binary was changed.

## Accuracy against the existing labels

| Gate | Development: skipped / missed relevant | Historical: skipped / missed relevant | Unseen instruction families: skipped / missed relevant |
| --- | ---: | ---: | ---: |
| Trained GLiClass, selected BF16/Metal | 48/272; 4/147 | 15/160; 2/88 | 81/192; 10/99 |
| Pretrained GLiClass, selected BF16/Metal | 25/272; 2/147 | 7/160; 1/88 | 50/192; 5/99 |
| **Jev-Style 0.8B v3 Q4, CPU** | **8/272; 0/147** | **0/160; 0/88** | **0/192; 0/99** |
| **Mapika Decider 2B v12 Q4, Metal** | **1/272; 0/147** | **0/160; 0/88** | **0/192; 0/99** |

“Skipped” includes missed relevant cases; subtract the misses to get correct skips. On the historical and unseen-instruction sets, both decision models caught every relevant exchange **because they reviewed every exchange**, including all irrelevant ones. The older trained MiniLM skipped 37/160 historical reviews with 2/88 relevant misses in its earlier experiment; it was not scored on this 192-case unseen-family set.

All four main rows use the same 272 development, 160 historical, and 192 authored unfamiliar-instruction cases and audited labels. The historical set activates four instructions per exchange; each authored unfamiliar case activates one. These sets were already inspected in previous experiments. They are a reused comparison, not a fresh held-out performance claim or production guarantee.

The decision models were used pretrained, with no local weight updates. The fixed question was: “Does the NEW EXCHANGE contain information that warrants review under this saved instruction: {instruction}? Apply the instruction's own criteria. PREVIOUS CONTEXT only clarifies references; old information alone does not count.” Up to 64 tokens from the preceding exchange were marked as previous context. Approved examples were omitted, matching the selected GLiClass configurations. User instructions were supplied directly, without forcing a fixed issue/API category.

Jev-Style read each full new exchange in a 4,096-token CPU context and scored instructions with its native Noul readout. Decider's selected format used the earlier GLiClass passage layout: 256-token passages with 64-token overlap, maximum score across passages, and a separate yes/no Choice question per instruction. Its whole-exchange development trial skipped 0/272; passage scoring improved this to 1/272. The compact 1,024-token Metal context produced **exactly the same 1,680 instruction scores** as the 4,096-token passage run across all three sets. Thresholds were selected from development only, then frozen before historical and unfamiliar-family scoring: 0.03861847 for Jev-Style and 0.03888066 for Decider.

Per-instruction checks show one API-positive historical pair below Jev-Style's threshold and one architecture-positive development pair below Decider's threshold. Other active instructions still triggered review on those exchanges, so combined missed-review counts are zero. A future gate with only that instruction active would miss them. At a default 0.5 probability threshold on development, Jev-Style missed 94/147 relevant exchanges and Decider's whole-exchange format missed 103/147; using raw 0.5 would be unsafe here.

## RAM and laptop fit

Measurements ran on an **M5 Max with 128 GiB unified memory**, one model at a time. Weight files are 529 MB for Jev-Style Q4 and 1.274 GB for Decider Q4. Weight size is not runtime memory use.

| Configuration | Measured memory | Practical reading |
| --- | --- | --- |
| Jev-Style 0.8B Q4, CPU, 4,096-token context | **2.45 GB peak sampled combined resident memory** for Python and its scorer | Likely fits in an 8 GB Apple Silicon Mac with light other use; 16 GB has more headroom. CPU scoring had a 2.18 s median per historical exchange on the M5 Max, so older Macs may be noticeably slower. |
| Decider 2B Q4, Metal, 1,024-token passage context | **2.30 GB peak process resident memory**. Runtime logged **1,205 MiB Metal model buffer**, **988 MiB Metal compute buffer**, 12 MiB KV cache, 19 MiB recurrent state, and 398 MiB CPU-mapped model buffer. | 8 GB may fit with limited headroom; **16 GB is the safer target** when other apps are open. On the M5 Max, median historical scoring was 0.17 s per exchange. |
| Decider 2B Q4, Metal, 4,096-token context | **2.41 GB peak process resident memory**; Metal compute buffer about **1,972 MiB** | The long context costs about 1 GiB more reserved Metal compute space without changing any score for the passage format. Do not use it for this gate. |

Process resident memory and Metal allocations can overlap on unified-memory Macs; **do not add the columns into an exact physical-RAM total**. The runtime reports buffer reservations, which are not a direct measurement of system memory pressure. An actual 8 GB Mac was unavailable, so the 8 GB assessments are feasibility estimates, not verified compatibility or speed. The 16 GB recommendation is conservative. Neither model justifies this background footprint given its near-zero review savings.

For context, the prior GLiClass fine-tuning report measured about 1.24 GB process resident memory after scoring and a 2.63 GB resident loading peak, with approximately 2.22 GB of driver allocations. Its counters can also overlap and are not directly comparable to a CPU-only worker.

## Reproduction and limits

The [scorer](system_one_evaluate.py) reads the frozen `.local/gliclass-finetuning/{development,historical,heldout}.json` partitions, checks their manifest hashes, records per-case scores and process use under `.local/system-one-evaluation/`, and selects the same 95% development recall criterion used for GLiClass. The Jev-Style local runner was limited to a 4,096-token context and built without Metal because Metal initialization is unavailable in the execution sandbox. Decider ran outside that sandbox with local Metal access. Both models used four CPU threads where relevant. The installed model revisions and source cards are recorded in [the research note](SYSTEM_ONE_MODEL_RESEARCH.md).

The fixed question tested whether an exchange warranted review and omitted approved examples. It was more demanding than the isolated detection question the user intended; the follow-up linked above corrects that mismatch. This initial run alone is not a verdict on the simpler formulation. The authored unfamiliar cases are synthetic and already inspected; a later tuned candidate needs a new held-out family set. A true 8 GB Mac should be used to verify memory pressure and latency before claiming that device class as supported.
