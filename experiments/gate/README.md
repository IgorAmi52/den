# Local gate experiment

The original offline comparison of local classifiers against DEN's Luna gate. The selected ModernBERT SetFit model now powers the live gate; see the [runtime, logs, preserved data, and handoff](../../docs/classifier-gate.md). These scripts remain offline experiment tools.

See [the measured results](RESULTS.md) for the completed experiment.

The target is whether the visible exchange warrants a careful card review. It is not whether the user would accept a card. Overall accuracy selects models and thresholds; precision, recall, macro F1, and confusion matrices describe the errors.

## Run

Use Python 3.12. Keep the dataset, call logs, and weights outside the repository: they contain private conversations. `prepare.py` reads local Claude and Codex histories, including archived Codex sessions. It excludes helper/subagent sessions, temporary test sessions, injected instructions, and the current working directory. It does not modify source transcripts.

```sh
experiment_directory=/private/tmp/den-gate-experiment
uv venv --python 3.12 "$experiment_directory/venv"
uv pip install --python "$experiment_directory/venv/bin/python" -r experiments/gate/requirements.txt
python3 experiments/gate/prepare.py --output "$experiment_directory/data"
python3 experiments/gate/label.py labels --data "$experiment_directory/data"
python3 experiments/gate/label.py labels --data "$experiment_directory/data/logged"
python3 experiments/gate/label.py baseline --data "$experiment_directory/data"
python3 experiments/gate/label.py baseline-folder --data "$experiment_directory/data"
python3 experiments/gate/label.py audit --data "$experiment_directory/data" --workers 2

for encoder in bge-small modernbert qwen; do
  "$experiment_directory/venv/bin/python" experiments/gate/train.py embed --encoder "$encoder" --data "$experiment_directory/data"
  "$experiment_directory/venv/bin/python" experiments/gate/train.py fit --encoder "$encoder" --data "$experiment_directory/data"
done
"$experiment_directory/venv/bin/python" experiments/gate/train.py setfit --encoder bge-small --data "$experiment_directory/data"
"$experiment_directory/venv/bin/python" experiments/gate/train.py setfit --encoder modernbert --data "$experiment_directory/data"
"$experiment_directory/venv/bin/python" experiments/gate/train.py nli --data "$experiment_directory/data"
"$experiment_directory/venv/bin/python" experiments/gate/train.py nli --encoder deberta --data "$experiment_directory/data"
"$experiment_directory/venv/bin/python" experiments/gate/evaluate.py --data "$experiment_directory/data"
```

Labeling uses the logged-in Codex CLI: `gpt-6.1-sol` at high reasoning effort. The baseline uses `gpt-6-luna` without reasoning and extracts the actual gate instructions from the Go source. Calls disable tools and session persistence. All prompts, answers, model names, usage, and timings are retained outside the repository. Successful calls are cached; interrupted runs resume. The independent audit uses `gpt-6-astra`, without seeing the first model's labels or any classifier predictions.

## Methods

| Method | What learns from DEN examples |
| --- | --- |
| Frozen embeddings + logistic regression | A binary classification head; regularization and threshold selected on validation data |
| Semantic Router style baseline | Labeled exemplars; nearest-example aggregation and threshold selected on validation data |
| SetFit | Contrastive fine-tuning of the encoder, then a logistic classification head |
| Zero-shot NLI | Three fixed descriptions of durable information; also reported with a validation-calibrated threshold |
| Current gate | Existing DEN system prompt and model; one call per held-out example |
| Majority baseline | Always predicts the most common training label |

The routing baseline implements cosine similarity against labeled routes directly; it does not depend on the Semantic Router package. Similarity scores are not calibrated probabilities. SetFit uses two contrastive sampling iterations, one epoch, and a learning rate of `1e-5`. It receives only training examples. The logistic head is selected using validation accuracy, with macro F1 breaking ties.

## Evaluation boundaries

- Entire conversations stay in one split. Forks, matching long first prompts, and exact repeated substantive exchanges connect conversations before splitting. Exact input duplicates are removed. The split seed and dataset hashes are saved.
- The encoder and method are selected by validation results. The test set is used only for the final comparison. Accuracy differences use a paired bootstrap over conversation groups, not individual messages.
- Historical examples use one completed exchange, with the first 700 characters of the prompt and last 700 of the reply, preserving formatting. This approximates the production adapters' character budget; it is not an exact reconstruction of live scheduling. The session goal comes from the initial prompt, never a future summary.
- Historical folder assignments, waiting-card state, and previous review boundaries cannot reliably be reconstructed. Inputs explicitly mark the folder unknown and show no waiting cards. Consequently this benchmark tests retention decisions, not card withdrawal, deduplication against notes, or the full scheduling pipeline.
- `baseline-folder` is a sensitivity control: it changes only the unknown folder marker to the neutral name `Work`. It does not reconstruct actual folder history or alter any real folder.
- Labels describe the available excerpts, not unseen full transcripts. Historical plan-approval flags are not reconstructed. The current local rules are not the primary baseline; the model gate is.
- Scores measure agreement with strong-model labels. The second-model audit estimates label consistency; neither model is a human ground truth. Data comes from one user's history across projects, not independent users.
- Original logged gate prompts are preserved separately and can be labeled for a sanity check. They are not an independent test set: their sessions may overlap training, and their buffered multi-exchange inputs differ from the message-level benchmark.

## Encoder shortlist

- [BGE small English v1.5](https://huggingface.co/BAAI/bge-small-en-v1.5): 33M parameters, 384 dimensions, 512 tokens. Compact baseline and practical SetFit starting point. The model card says non-retrieval tasks do not need its retrieval instruction.
- [ModernBERT embed base](https://huggingface.co/nomic-ai/modernbert-embed-base): approximately 149M parameters, 768 dimensions, longer context. Uses the required `classification:` prefix. It is an embedding model; the original masked-language ModernBERT checkpoint is not a ready-made sentence encoder.
- [Qwen3 Embedding 0.6B](https://huggingface.co/Qwen/Qwen3-Embedding-0.6B): 0.6B parameters, 1,024 dimensions, 32K native context, instruction support, and multilingual/code capability. This experiment caps it at 1,024 tokens because the inputs are short. Its larger memory footprint must earn its place through accuracy.
- [ModernBERT zero-shot v2](https://huggingface.co/MoritzLaurer/ModernBERT-base-zeroshot-v2.0): an NLI classifier, not an embedding model. Its author reports a speed advantage over DeBERTa, with somewhat lower average accuracy on the published tasks.
- [DeBERTa v3 base zero-shot v2-c](https://huggingface.co/MoritzLaurer/deberta-v3-base-zeroshot-v2.0-c): a second NLI candidate because accuracy matters more here than throughput. The author identifies the `-c` checkpoints as trained on commercially friendly data.

General embedding leaderboard scores do not establish which model best detects durable coding decisions. This experiment measures that task directly.

## Offline prediction

For a trained logistic classifier, pass a gate prompt on standard input:

```sh
"$experiment_directory/venv/bin/python" experiments/gate/predict.py \
  --model "$experiment_directory/data/bge-small" < gate-prompt.txt
```

It returns `worthReview`, the classifier score, and its validation-selected threshold. Loading uses local weights only. A frozen encoder needs its downloaded Hugging Face cache; a directory containing a saved `model/` is self-contained. The score is not a calibrated confidence estimate.

Add `--router` to use that encoder's saved exemplar-routing classifier instead of its logistic head.

References: [SetFit](https://huggingface.co/docs/setfit/index), [Semantic Router](https://github.com/aurelio-labs/semantic-router), [zero-shot classifiers](https://github.com/MoritzLaurer/zeroshot-classifier).
