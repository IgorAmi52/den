# Production gate verification — 2026-10-23

The selected model is now used by the live Go gate. The reviewer remains Sol. Setup, logs, data locations and future training are described in [the handoff](../../docs/classifier-gate.md).

## Verification

- Reconstructed all 312 held-out examples through Go's production input builder. All 312 inputs matched the training-format inputs exactly, including formatting.
- Replayed them through the installed Python runtime from the actual Go classifier function. All 312 decisions matched the archived predictions; 251 were correct against the original labels (80.4487%). Maximum score difference was 0.0000001192.
- The successful 312-example Metal replay took 5.698 seconds including startup, artifact verification and model loading. A separate single-exchange invocation took 3.38 seconds. These are shared-machine measurements, not a latency guarantee. The first cold full replay hit the 90-second timeout; subsequent single and full replays completed. Inference timeouts allow Sol review and are recorded.
- Verified a real negative prediction remains negative; approved-plan, markdown-write and pending-card overrides each allow review while retaining the negative raw prediction.
- Verified a missing runtime allows review and records the inference error.
- Focused scheduling checks covered new replies during positive and negative gate calls, review boundaries and gate-to-review provenance, plus classifier fallback leaving reviewer failure accounting unchanged.
- Verified all 5,465 archived data/cache files against their SHA-256 hashes.
- Go package checks, vet, Python parsing and whitespace checks are part of the final merge validation. The repository has no pre-existing unit tests; the focused rollout checks were run separately and their source retained in the private rollout archive.

Model manifest version: `777693bd0923fbdcbc3054b10a33370ae4f5516763d5c3fa27552a0e945a375b`.

Private replay inputs, outputs, verification harnesses and the summary are saved in `.local/gate-experiment/rollout/`. These checks do not call Sol or generate cards in real sessions. No fresh live-accuracy claim is made; production logs are the next evaluation dataset.
