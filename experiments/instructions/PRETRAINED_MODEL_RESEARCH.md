# Pretrained local gate candidates — 2026-10-30

Evaluate **GLiClass v4 Edge** and **Qwen3-Reranker-0.6B** first. Compare them with **Granite 4.0 350M** and **GTE reranker ModernBERT base** to find out whether a smaller model is sufficient. Keep all weights unchanged for this comparison.

GLiClass is the closest documented match to our task: applying a supplied rule to text without generating an answer. Qwen is an instruction-aware reranker with an existing small quantized artifact. These are research priorities, not measured Den performance rankings. No new model inference, evaluation, fine-tuning, installation, or live Den changes were performed for this report; only public documentation and metadata were downloaded.

The user's constraint is that Den should work on less powerful machines than the development Mac. Assume an ordinary **8–16 GB Mac running other applications**, and prefer models below one billion parameters. The development Mac's 128 GiB memory is not a deployment budget.

## What we need to predict

The input is a saved instruction, optional approved examples, and new session evidence. The output is whether to let Sol review that evidence. This includes changes to earlier findings when the instruction requests maintenance. It does not require deciding the final proposal, folder, or write.

The difficult distinction is instruction satisfaction, not topic similarity. Under “capture confirmed external API limits,” an actual provider restriction should match; a suggestion to investigate possible restrictions should not. Under a broader instruction to track suspected issues, uncertain evidence may legitimately match. These distinctions must follow the supplied instruction, rather than a hardcoded confirmed-facts policy.

We should use the new exchange as the default evidence. For reference-dependent updates, test explicitly separated minimal previous context. Do not restore the entire conversation as an undifferentiated matching document. Pending-card maintenance must retain its existing direct-review path.

The previous experiment tested MiniLM MS MARCO, ModernBERT zero-shot, and DeBERTa zero-shot. None avoided more than one of 192 development reviews under its recall criterion. Those results do not cover the candidates below. The GTE checkpoint shares the ModernBERT architecture with an earlier candidate but has different retrieval training. See [the previous experiment](CROSS_ENCODER_RESULTS.md).

## Shortlist

Artifact sizes below use decimal MB and describe weights only. They exclude the inference runtime, tokenizer, activations, caches, and installation dependencies. They are **not RAM measurements**. Quantized versions need their own accuracy check.

| Candidate | Parameters | Available weight artifact | Reason to evaluate | Main uncertainty |
| --- | ---: | --- | --- | --- |
| [GLiClass v4 Edge](https://huggingface.co/knowledgator/gliclass-v4-edge) | 440M | 882 MB, mostly BF16 | Accepts a policy and labels at inference time; supports examples; returns label scores without text generation | New checkpoint; Python/PyTorch footprint and CPU latency; rule following on Den data |
| [Qwen3-Reranker-0.6B](https://huggingface.co/Qwen/Qwen3-Reranker-0.6B) | 596M | [639 MB Q8 GGUF](https://huggingface.co/ggml-org/Qwen3-Reranker-0.6B-Q8_0-GGUF) | Explicit custom task instruction; joint instruction/passage scoring; native yes/no scoring | Retrieval relevance may still over-trigger on related topics; correct custom prompt support in the chosen runtime |
| [Granite 4.0 350M](https://huggingface.co/ibm-granite/granite-4.0-350m) | 352M | [237 MB Q4_K_M or 378 MB Q8 GGUF](https://huggingface.co/ibm-granite/granite-4.0-350m-GGUF) | Very small instruction model; can read the gate policy directly; publisher provides quantized artifacts | Small models can misread negation, status updates, or compound rules; generation adds another failure mode |
| [GTE reranker ModernBERT base](https://huggingface.co/Alibaba-NLP/gte-reranker-modernbert-base) | 150M | Approximately 598 MB at the published FP32 parameter precision | Small encoder baseline; English/code retrieval; 8,192-token advertised context | Ordinary relevance training, with less evidence of arbitrary policy following |

All four have Apache-2.0 model licenses in their publisher metadata. Licensing is not a differentiator among these four.

### GLiClass v4 Edge: closest task match

The documented interface supports a policy in `prompt`, descriptive output labels, and optional examples. That permits a direct experiment with “new evidence warrants review under this saved instruction” versus “new evidence does not warrant review.” It does not require a fixed list of issue/API/architecture categories or training for each new user instruction.

The model can score several labels together. That does not establish that placing many lengthy Den instructions in one call will retain the accuracy of separate decisions; measure both before relying on batching savings.

Its advertised 11 ms median is from **an RTX PRO 6000 GPU, BF16, batch size eight**, on the publisher's Decision Index evaluation. It is not CPU or Mac performance. Likewise, published calibration is not calibration on Den's data.

The checkpoint contains a DeBERTa-based encoder. The card's example uses 2,048 tokens, its benchmark uses a larger limit, and the configuration contains 512-position settings with relative attention. We should verify actual input behavior and memory scaling, not infer an inexpensive long-context limit from those examples. Start with bounded passages that cover the full exchange.

The published weights are about 0.88 GB, but FP32 weights alone would be about 1.76 GB. Whether it fits a small total worker-memory budget depends on the supported precision and runtime. CPU is documented; an efficient Metal path is not established by this research. The current `gliclass` package also requires Transformers 5 and NumPy 2, so evaluate in an isolated environment rather than upgrading Den's current worker dependencies.

### Qwen3-Reranker-0.6B: practical reranker candidate

The publisher documents separate `Instruct`, `Query`, and `Document` fields and a score computed from the final yes/no logits. We can put the fixed gate rule in `Instruct`, the saved user instruction and examples in `Query`, and the marked new evidence in `Document`. No generated explanation is required.

It advertises 32K context and over 100 languages, but we should not allocate a 32K context for a background gate by default. Long evidence and multiple instructions still require computation even when the output is one score.

The ggml-org Q8 conversion provides a concrete 639 MB deployment candidate. llama.cpp documents a reranking endpoint and CPU/Metal execution is a plausible deployment route. **The existence of a GGUF file does not verify that the chosen endpoint preserves Qwen's custom instruction and yes/no scoring template.** Check parity with the publisher's reference implementation before interpreting failures as model limitations.

The publisher reports instruction-following retrieval benchmarks, which is relevant to selecting it for evaluation. Those benchmarks do not establish Den recall, threshold calibration, or status-update detection.

### Granite 350M and GTE 150M: resource controls

Granite is the small general instruction-model comparison: request a minimal review/skip answer without an explanation. Invalid or incomplete output must allow review and count against savings. A token score, if supported, can be calibrated on development data; neither a self-reported confidence value nor a forced binary answer proves reliability. IBM explicitly lists classification among intended uses and supplies GGUF variants.

GTE is the smaller encoder comparison. Its purpose is to test whether stronger policy-following models earn their extra resource use. Do not assume that its longer context or better retrieval benchmarks solve the topic-versus-evidence failure we already observed.

## Other options considered

| Model or family | Why it is outside the initial shortlist |
| --- | --- |
| [Qwen3.5-0.8B](https://huggingface.co/Qwen/Qwen3.5-0.8B) | Reasonable reserve if Granite cannot follow the rule. A [community 4-bit MLX artifact](https://huggingface.co/mlx-community/Qwen3.5-0.8B-4bit) has 625 MB of weights. Larger than Granite by parameter count; use non-thinking mode and text only if tested. The publisher describes this small release as intended for prototyping, task-specific fine-tuning, and research/development. |
| [GLiNER2.5-Decide](https://huggingface.co/fastino/GLiNER2.5-Decide) | Relevant operational classifier and a reserve for GLiClass. The card calls it 340M, while checkpoint metadata totals 486M parameters and 1.95 GB FP32 weights. Its documented specialist classification interface does not establish arbitrary compound-rule handling. Do not compare it using the smaller advertised count alone. |
| [Mixedbread rerank base v2](https://huggingface.co/mixedbread-ai/mxbai-rerank-base-v2) | Approximately 494M parameters; useful retrieval alternative, but less direct evidence of custom-policy support than the shortlisted Qwen. Defer to avoid testing many similar rerankers before learning from the first comparison. |
| [BGE reranker v2 m3](https://huggingface.co/BAAI/bge-reranker-v2-m3) | Approximately 568M parameters and general query/document relevance. Useful mature baseline, but does not offer a clear task-fit advantage over the initial options. |
| [Liquid LFM2.5-230M](https://huggingface.co/LiquidAI/LFM2.5-230M) | Especially small instruction model with publisher MLX/GGUF support. Its [license](https://huggingface.co/LiquidAI/LFM2.5-230M/blob/main/LICENSE) has a commercial revenue condition, unlike Apache-2.0; prioritize simpler redistribution options for Den. Published token-generation speed does not measure gate latency. |
| [Jina reranker v3](https://huggingface.co/jinaai/jina-reranker-v3) | Approximately 597M parameters, but CC BY-NC 4.0 weights add commercial licensing constraints. |
| Qwen rerankers 4B/8B, Zerank 2 4B, larger chat models | Outside the lightweight default the user requested. |
| [Gemma 4 E2B](https://huggingface.co/google/gemma-4-E2B-it) | The small-sounding name describes effective computation; published total parameters are about 5.1B including embeddings. Outside the initial memory-focused shortlist. |

## Proposed evaluation, before considering training

1. **Check resource feasibility.** Measure each isolated worker's cold start, complete download/install footprint, idle and peak resident memory, and warm median/p95 latency. Include CPU execution with a fixed small thread count, then Metal where supported. Use short, typical, and long exchanges with one, four, and twelve instructions. Record actual tokens and full-exchange time rather than only per-window time. Thread limiting on the development Mac does not simulate an older Mac; an ordinary Mac is needed to validate the final deployment claim.
2. **Use existing data for development.** Reuse the historical labels and 48 diagnostic cases to check input formatting, runtime parity, and thresholds. They are already inspected, so they cannot be described as fresh held-out evidence. Keep the trained new-only model and direct Sol review as baselines. Freeze templates, checkpoint revisions, quantization, passage handling, and thresholds before the final evaluation. This is configuration/calibration, not fine-tuning.
3. **Create a fresh instruction-generalization evaluation.** Hold out whole instruction families from prompt/example/threshold development, including nontechnical ones. Include identical passages paired with different instructions, suspected versus confirmed findings where the instruction distinguishes them, proposals versus accepted decisions, deferrals versus routine next steps, old findings followed by unrelated work, and actual resolutions/cancellations. Keep related conversations and rewrites in one partition. Mix fresh historical source groups, where available, with explicitly identified authored challenges; report them separately. Audit labels and ambiguous cases independently of predictions. Previously untouched API examples alone would not answer the generalization question.
4. **Choose on missed findings and useful savings.** Report relevant exchanges missed per instruction and per unseen family, unnecessary reviews, end-to-end reviews avoided after batching/fallbacks, memory, and latency. Show intervals and counts: a few positive cases cannot establish reliable 95% recall. Retain the prior 95% recall screen for development comparisons, not as a proven production guarantee. Re-run quality checks for the actual quantized artifact rather than assuming full-precision results transfer.

A proposed resource screen is **at most about 2 GB total steady-state worker memory**, preferably substantially less, with **under one second warm p95 for a typical exchange against four instructions** on the target ordinary Mac. These are suggested engineering targets, not agreed product requirements or measured results. Cold-start and long-exchange costs must also be visible. A model that cannot meet a reasonable memory budget should not become a mandatory background process.

Only consider fine-tuning if a feasible pretrained candidate shows a repeatable Den-specific error that prompt formatting, examples, and threshold calibration do not fix. If it is accurate but too large, a smaller model/distillation experiment is a separate justified question. If no lightweight candidate avoids enough reviews at acceptable recall, retain direct review rather than shipping a larger compulsory model or weakening recall silently.

## Reproduction references

Publisher cards, configurations, and metadata were retrieved on 2026-10-30. The ignored `.local/pretrained-gate-research/` directory contains the downloaded source snapshots; no session content was sent to these sites.

| Checkpoint | Observed revision |
| --- | --- |
| `knowledgator/gliclass-v4-edge` | `885af1af8a07edf4ed34df83ca0d995ed8201283` |
| `Qwen/Qwen3-Reranker-0.6B` | `e61197ed45024b0ed8a2d74b80b4d909f1255473` |
| `ggml-org/Qwen3-Reranker-0.6B-Q8_0-GGUF` | `a02f48bb4f057028298c21fa033da2b30d7742d5` |
| `ibm-granite/granite-4.0-350m-GGUF` | `b8208a86a58427e1739265318028eb5895b74bf2` |
| `Alibaba-NLP/gte-reranker-modernbert-base` | `f7481e6055501a30fb19d090657df9ec1f79ab2c` |

Runtime references: [GLiClass](https://github.com/Knowledgator/GLiClass), [llama.cpp server](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md), and [MLX LM](https://github.com/ml-explore/mlx-lm). Runtime support and exact prompt preservation still need verification during evaluation.
