"""Kokoro TTS sidecar.

Small FastAPI service the Go backend calls when generating audio chunks
for a node. Two engines are loaded on startup:

  - KPipeline (PyTorch) — upstream Kokoro-82M, used for en/es/fr/it.
    Supports all voices from the official model.
  - kokoro_onnx.Kokoro — runs the community German Martin fine-tune,
    which only ships as ONNX. Single voice.

Synthesized PCM is piped through ffmpeg to produce Opus-in-OGG, returned
as application/ogg with an X-Audio-Duration-Ms header.
"""

from __future__ import annotations

import io
import logging
import subprocess
from contextlib import asynccontextmanager
from pathlib import Path
from typing import Literal

import numpy as np
import soundfile as sf
from fastapi import FastAPI, HTTPException, Response
from pydantic import BaseModel, Field

LOG = logging.getLogger("tts")
logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s")

MODELS_DIR = Path(__file__).resolve().parent / "models"

# Two-letter ISO code -> single-letter Kokoro lang code used by KPipeline.
KOKORO_LANG: dict[str, str] = {
    "en": "a",  # American English
    "es": "e",
    "fr": "f",
    "it": "i",
}

# Lazy module imports inside lifespan() so import errors at boot don't
# crash uvicorn's reloader before we get a chance to log them.
pipelines: dict[str, object] = {}  # lang two-letter -> KPipeline
de_kokoro = None  # type: ignore[var-annotated]


@asynccontextmanager
async def lifespan(app: FastAPI):
    global de_kokoro

    import onnxruntime as ort
    from kokoro import KPipeline
    from kokoro_onnx import Kokoro

    LOG.info("loading multi-language Kokoro pipelines")
    for lang_code, kokoro_code in KOKORO_LANG.items():
        LOG.info("  - %s (lang=%s)", lang_code, kokoro_code)
        pipelines[lang_code] = KPipeline(lang_code=kokoro_code)

    LOG.info("loading German Martin ONNX model")
    martin_dir = MODELS_DIR / "kokoro-de-martin"
    # The Martin fine-tune ships voices as a numpy .npz archive (not the
    # custom .bin format kokoro_onnx normally consumes), so we have to
    # build an InferenceSession by hand and hand it to from_session.
    session = ort.InferenceSession(
        str(martin_dir / "kokoro-martin.onnx"),
        providers=["CPUExecutionProvider"],
    )
    de_kokoro = Kokoro.from_session(session, str(martin_dir / "voices-martin.npz"))
    LOG.info("German Martin loaded with %d voice(s)", len(de_kokoro.get_voices()))

    yield

    pipelines.clear()
    de_kokoro = None


app = FastAPI(lifespan=lifespan, title="Mindful Social TTS sidecar")


class SynthesizeRequest(BaseModel):
    text: str = Field(min_length=1, max_length=20_000)
    language: Literal["en", "es", "fr", "it", "de"]
    voice: str = Field(min_length=1, max_length=64)
    speed: float = Field(default=1.0, ge=0.5, le=2.0)


@app.get("/healthz")
def healthz():
    return {
        "ok": True,
        "languages": sorted([*KOKORO_LANG.keys(), "de"]),
    }


@app.post("/synthesize")
def synthesize(req: SynthesizeRequest) -> Response:
    if req.language == "de":
        samples, sample_rate, voice = _synth_german(req.text, req.speed)
    else:
        samples, sample_rate, voice = _synth_multi(
            req.text, req.language, req.voice, req.speed,
        )

    samples = np.asarray(samples, dtype=np.float32)
    duration_ms = int(round(1000 * len(samples) / sample_rate))

    wav_buf = io.BytesIO()
    sf.write(wav_buf, samples, sample_rate, format="WAV", subtype="PCM_16")
    opus_bytes = _encode_opus(wav_buf.getvalue())

    return Response(
        content=opus_bytes,
        media_type="audio/ogg",
        headers={
            "X-Audio-Duration-Ms": str(duration_ms),
            "X-Audio-Sample-Rate": str(sample_rate),
            "X-Audio-Voice": voice,
        },
    )


def _synth_multi(text: str, language: str, voice: str, speed: float):
    pipeline = pipelines.get(language)
    if pipeline is None:
        raise HTTPException(400, f"unsupported language: {language}")
    try:
        # KPipeline yields per-sentence chunks; concatenate them so the
        # caller gets a single contiguous WAV per /synthesize call. The
        # chunking we care about (paragraph-level) happens server-side
        # before this call, so per-sentence breaks would just produce
        # noisy clicks between segments.
        audio_chunks = []
        sample_rate = 24000
        for _, _, audio in pipeline(text, voice=voice, speed=speed):
            audio_chunks.append(np.asarray(audio, dtype=np.float32))
        if not audio_chunks:
            raise HTTPException(500, "kokoro produced no audio")
        return np.concatenate(audio_chunks), sample_rate, voice
    except HTTPException:
        raise
    except Exception as exc:  # noqa: BLE001
        LOG.exception("multi-language synthesis failed")
        raise HTTPException(500, f"synthesis failed: {exc}") from exc


def _synth_german(text: str, speed: float):
    if de_kokoro is None:
        raise HTTPException(500, "german model not loaded")
    try:
        voice = de_kokoro.get_voices()[0]  # Martin — the only one
        samples, sample_rate = de_kokoro.create(
            text, voice=voice, speed=speed, lang="de",
        )
        return samples, sample_rate, voice
    except Exception as exc:  # noqa: BLE001
        LOG.exception("german synthesis failed")
        raise HTTPException(500, f"synthesis failed: {exc}") from exc


def _encode_opus(wav_bytes: bytes) -> bytes:
    # libopus at 32 kbps VBR is a sweet spot for speech: small files, no
    # audible quality loss vs the source 24kHz mono WAV. We pipe in/out so
    # we never touch the filesystem.
    proc = subprocess.run(
        [
            "ffmpeg", "-loglevel", "error", "-y",
            "-f", "wav", "-i", "pipe:0",
            "-c:a", "libopus", "-b:a", "32k", "-vbr", "on",
            "-application", "voip",
            "-f", "ogg", "pipe:1",
        ],
        input=wav_bytes,
        capture_output=True,
        check=False,
    )
    if proc.returncode != 0:
        LOG.error("ffmpeg failed: %s", proc.stderr.decode("utf-8", "replace"))
        raise HTTPException(500, "opus encoding failed")
    return proc.stdout
