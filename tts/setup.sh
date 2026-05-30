#!/usr/bin/env bash
# Bootstrap the Kokoro TTS sidecar:
#   1. Create / sync the uv-managed virtualenv at tts/.venv
#   2. Pre-fetch the German Martin ONNX model into tts/models/
#   3. Pre-warm the upstream Kokoro-82M cache (downloads ~330MB into
#      HuggingFace's cache so the first /synthesize call isn't slow).
#
# Re-runs are idempotent and cheap. Run once after cloning, and again
# whenever pyproject.toml changes. Requires the nix devShell (uv +
# espeak-ng + python3 + ffmpeg).
set -euo pipefail

cd "$(dirname "$0")"

echo "==> syncing python venv with uv"
uv sync

export MODELS_DIR="$PWD/models"
mkdir -p "$MODELS_DIR"

echo "==> downloading German Kokoro (Martin) ONNX model"
uv run python - <<'PY'
import os
from pathlib import Path
from huggingface_hub import hf_hub_download

dest = Path(os.environ["MODELS_DIR"]) / "kokoro-de-martin"
dest.mkdir(parents=True, exist_ok=True)

for filename in ["kokoro-martin.onnx", "voices-martin.npz"]:
    local = hf_hub_download(
        repo_id="huggingFresse/Kokoro-82M-ONNX-German-Martin",
        filename=filename,
        local_dir=str(dest),
    )
    print(f"  {filename} -> {local}")
PY

echo "==> pre-warming upstream Kokoro-82M cache"
uv run python - <<'PY'
# Instantiating KPipeline triggers the HF download of hexgrad/Kokoro-82M
# (model file + tokenizer assets). We do it for each lang code once so
# the misaki phonemizer assets for that language are cached too.
from kokoro import KPipeline
for lang in ("a", "e", "f", "i"):
    print(f"  warming lang={lang}")
    KPipeline(lang_code=lang)
PY

echo "==> done. Run ./tts/run.sh to start the sidecar."
