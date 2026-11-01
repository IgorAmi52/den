# Local System One decision models — 2026-11-01

Evaluate **Jev-Style 0.8B Decision v3** and **Mapika Decider 2B** first, with **WaterSheep** as a small CPU baseline. Keep GLiClass as the existing comparison. These are research priorities, not measured Den accuracy rankings. No new inference, model downloads, package installation, fine-tuning, or live gate changes were performed for this investigation. Only public documentation and metadata were downloaded.

The deployment target remains an ordinary 8–16 GB Mac running other applications. A model fitting the development machine does not establish that it is suitable as Den's background gate.

## Fit for Den

The user's [TypeSafe Jev article](https://typesafe.ai/blog/introducing-system-one-models-and-jev) describes a relevant interface: provide state and questions, receive typed decisions directly without generated explanations. Its [documentation](https://docs.typesafe.ai/introduction) supports Choice, Score, and Noul (a yes/no probability), and recommends atomic questions. TypeSafe's published offering is hosted; this investigation did not find official downloadable TypeSafe Jev weights. Open models named JEV, Jev-Style, and Kev are separate projects.

For Den, the state would contain the new exchange and, when needed, explicitly separated prior context. Each question would ask whether the new evidence warrants review under one saved user instruction and its approved examples. Sol would still determine the actual proposal and destination. This also covers changes to earlier findings when an instruction requests maintenance; the gate must not impose a universal confirmed-facts policy.

Returning one bit removes text generation and parsing, but does not make the semantic task trivial. The model still has to distinguish a real finding from a hypothetical example, follow the particular user's rule, and avoid matching only stale context. Input processing also costs time and memory. GLiClass already makes decisions without generating text; these alternatives offer different training and runtimes, not an automatic solution from the System One name alone.

## Available lightweight candidates

Sizes are decimal weight/artifact sizes, **not RAM or complete installation sizes**. Runtime buffers, tokenizer, input length, dependencies, and load-time copies add overhead. All candidates in this table declare Apache-2.0 model/project licenses. Jev-Style and Decider explicitly disclose separate or unclear training-source terms; the declared weight license is not a review of all training-data provenance.

| Model | Parameters | Verified available artifact | Runtime and assessment |
| --- | ---: | --- | --- |
| [WaterSheep](https://huggingface.co/samratduttaofficial/WaterSheep) | 154M | 158 MB quantized ONNX; 617 MB full weights | ModernBERT encoder with a decision head. CPU baseline; very small download. English only, long inputs truncate. Publisher reports 61.2% accuracy on held-out datasets, so generalization is an open concern. |
| [Laya](https://huggingface.co/convaiinnovations/laya) | 421M | [449 MB Q8 GGUF](https://huggingface.co/ggml-org/Laya-GGUF); 843 MB BF16 | ModernBERT-large decision model. GGUF card documents a dedicated `/v1/systemone` endpoint. Base English configuration uses a short 512-token context; useful reserve if it can accommodate our input without losing evidence. |
| [Jev-Style 0.8B Decision v3](https://huggingface.co/chaoliangUNSW/Jev-Style-0.8B-Decision-v3-GGUF) | 752M | 529 MB Q4_K_M; 812 MB Q8 | First compact rule-reading candidate. Qwen3.5 text backbone, native decision scoring, bundled `jev-score` runtime with a documented Metal build. |
| [Kev 0.8B](https://huggingface.co/jaredpalmer/kev-0.8b) | 0.8B backbone | 43 MB adapter + 2 MB head **plus the required Qwen base** | Apple-native MLX serving; attractive reserve. Small adapter download does not represent total model memory. Publisher recommends 4B for quality and 0.8B when size matters. |
| [Mapika Decider 2B](https://huggingface.co/Mapika/decider-2b-GGUF) | 1.882B text | 1.274 GB Q4_K_M; 2.012 GB Q8 | Stronger-capacity comparison, designed for state plus typed questions. Dedicated `decider-ai` GGUF runtime documents CPU/Metal options. Publisher recommends Q8; evaluate Q4 separately for smaller deployment. |
| [Jev-Style 2B Decision v4](https://huggingface.co/chaoliangUNSW/Jev-Style-2B-Decision-v4-GGUF) | 2B family | 1.274 GB Q4_K_M; 2.012 GB Q8 | Reserve if 0.8B struggles. Requires `jev-score-v2` with block attention and per-option readout. An MLX release also exists. |

Mapika also publishes a [0.8B model](https://huggingface.co/Mapika/decider-0.8b) with 1.505 GB BF16 text weights, but no publisher GGUF release for that size was found. Its card warns about conditional instructions. Start with the already-packaged small Jev-Style artifact rather than adding a conversion project to the first comparison.

## Runtime and benchmark details that affect the decision

- **LM Studio chat is not the intended execution path.** Decider and Jev-Style read scores at specific positions, and some use custom attention. Loading their GGUF files as ordinary chat models does not reproduce their decision interface. Den can call a dedicated local worker, with no provider call, but runtime integration is still work. Laya's documented System One endpoint is also distinct from chat completion.
- **Decider's default yes/no scores need special care.** Its current 2B v12 card says Noul temperature is set to 0.05 for JevBench scoring, sacrificing calibration: reported calibration error rises from 0.095 to 0.341. The fitted Noul temperature is 3.185. Use the fitted setting or preserved raw decision scores, then select a Den threshold on development data. A score near one under the default does not establish certainty. The standalone GGUF script instead treats questions as Choice, so record the exact runtime and question type.
- **Jev-Style 0.8B's 240/240 quantization agreement is not 100% accuracy.** Those are training-pool parity fixtures. Its published Mac latency chart uses an untrained identical-architecture export in F16, not measured performance of the released Q4 checkpoint. The default scorer opens a 32,768-token context; verify and bound its allocation for Den.
- **Jev-Style 2B v4 needs Mac validation.** Its scorer was checked on Metal with the prior v3 model; the v4 GGUF files were checked on CUDA. Publisher Q4 top-answer agreement is 97.4%, with probability differences up to 0.321. Thresholds cannot simply be copied from full precision. The card recommends Q8/F16 for long documents.
- **Kev provides useful Mac measurements, but they are not Den measurements.** Its [runtime README](https://github.com/jaredpalmer/kev) reports 149 ms for five questions about approximately 270 new tokens on an M5 with 32 GB, and 28 ms with that same state cached. New-source test accuracy is 69.7% on one transfer suite and 58.6% on a broader held-out-dataset suite. These are different tasks and cannot be ranked against another model's unrelated benchmark. Adapter merging temporarily holds an extra weight copy.
- **Small encoder alternatives have limits.** [WaterSheep](https://github.com/SamratDuttaOfficial/WaterSheep) flags truncation; truncated evidence must not silently become a confident skip. [Laya's typed-decisions specialist](https://huggingface.co/convaiinnovations/laya-typed-decisions) is trained for four synthetic workflow families and warns it may perform worse elsewhere. Its headline gains do not establish arbitrary-instruction generalization.
- **Multiple questions are not automatically independent.** Some runtimes share state exactly; others document small changes when questions are batched. Verify agreement and actual savings before using batching for several Den instructions.

## Other market options

| Option | Why it is outside the first comparison |
| --- | --- |
| [AutoTrust JEV-9B](https://huggingface.co/autotrust/JEV-9B) | Independent Jev reproduction, not TypeSafe's released weights. Approximately 9B parameters and 18 GB BF16 weights before overhead; too large for this target. |
| [AutoTrust JEV-35B](https://huggingface.co/autotrust/JEV-35B), [GEV-26B](https://huggingface.co/autotrust/GEV-26B-Decide) | Mixture-of-experts active-parameter counts do not describe memory: all expert weights still need storage. |
| [Perplexity Decider Flash v1.2](https://huggingface.co/perplexity-ai/pplx-decider-flash-v1.2) | Public weights; about 4.54B total parameters and a 5.16 GB Q8 GGUF artifact. Uses noncausal attention and protocol-3 inline readout; a stock protocol-1 System One endpoint does not reproduce it. Larger and more involved than the initial target. |
| [Mapika Decider 4B](https://huggingface.co/Mapika/decider-4b-GGUF) | 2.71 GB Q4 / 4.48 GB Q8 weights. Reserve only if a measured quality gain justifies the footprint. |
| [Verdict](https://github.com/Manavarya09/verdict) | Very small E5-based bi-encoder with an optional decision head. Its own report notes weaknesses on conditional/typed tasks. Lower priority because this returns to the separately embedded text approach that has already struggled for Den. |
| [GLiNER2.5-Decide](https://huggingface.co/fastino/GLiNER2.5-Decide) | Relevant encoder reserve; metadata totals 486M parameters and 1.95 GB FP32 weights despite a smaller advertised count. Already covered in the prior research. |
| [StartLux 4B Instant](https://huggingface.co/startlux-models/StartLux-Decision-v1.1-4B-Instant-GGUF) | Around 2.71 GB Q4 weights and a CC-BY-NC-4.0 license; not the simplest redistribution option. |

## Next experiment

1. **Check runtime correctness and resources first.** Run the publishers' native decision interfaces for Jev-Style 0.8B, Decider 2B, and WaterSheep. Check reference/quantized agreement. Measure complete download footprint, cold start, idle/peak memory, and warm median/p95 latency on CPU and Metal where supported. Include one, four, and twelve instructions, and short/typical/long exchanges. Do not interpret the development Mac's timings as ordinary-Mac measurements.
2. **Configure without fine-tuning.** Use previously inspected Den data for prompt formatting, context policy, examples, and threshold selection. Keep the supplied instruction authoritative. Prefer calling Sol on uncertain cases; measure how much that fallback reduces savings. Preserve the existing direct-review path for pending-card maintenance.
3. **Freeze and evaluate unseen instructions.** Freeze checkpoint revision, runtime, precision, question template, and threshold. Add a genuinely fresh test containing whole instruction families absent from development, including nontechnical instructions. Include identical passages under contrasting instructions, old findings plus unrelated new text, hypotheses versus facts when relevant to the rule, references to earlier evidence, mixed exchanges, and resolutions. Report authored challenges separately from real session samples; keep related conversations in one split.
4. **Choose by recall and useful savings together.** Compare missed relevant exchanges per instruction/family, reviews avoided after fallbacks and batching, unnecessary reviews, latency, and memory against GLiClass. Report counts and uncertainty. The earlier 95% recall screen can remain a development criterion, not a demonstrated production guarantee. Reject a lightweight model if preserving recall eliminates meaningful savings.

Do not fine-tune merely because these models support it. Consider training only after a feasible pretrained model has a repeatable Den-specific failure and improvements can be verified on unseen instruction families. The previous GLiClass fine-tune improved selectivity while increasing misses on unfamiliar instructions; this comparison must test that weakness directly.

## Source snapshots

Publisher cards and Hugging Face artifact metadata were retrieved on 2026-11-01 and cached in ignored `.local/decision-model-research-2026-11-01/`. No Den session text was sent to these sites. All reported external accuracy and speed figures are publisher results, not this project's evaluation.

| Artifact | Observed revision |
| --- | --- |
| `samratduttaofficial/WaterSheep` | `236de6496b530d37e36a8ac63ff8412e92940f6e` |
| `ggml-org/Laya-GGUF` | `845f314dd047fc2444a5289a4d50c1e1d59e27e1` |
| `chaoliangUNSW/Jev-Style-0.8B-Decision-v3-GGUF` | `a51ab073b6a6a99fe2d718d674ad5439e35814c7` |
| `jaredpalmer/kev-0.8b` | `bf75a6a8848ea6960ff2ed108d9ed44c2941174f` |
| `Mapika/decider-2b` | `7614a6c7ea5a047aa7d7bcfb1a0015469381c3e3` |
| `Mapika/decider-2b-GGUF` | `0df001b8fe4e5298c5dd45d875d3795fcdaea0ab` |
| `chaoliangUNSW/Jev-Style-2B-Decision-v4-GGUF` | `d1b3dfd6f47e8e0b4b366c401e2309043fb6510c` |

Prior Den evidence: [pretrained evaluation](PRETRAINED_GATE_RESULTS.md), [GLiClass fine-tuning results](GLICLASS_FINETUNING_RESULTS.md), and [earlier candidate research](PRETRAINED_MODEL_RESEARCH.md).
