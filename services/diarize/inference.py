"""Offline Nemotron 3 Diarization. Float16 on CUDA, float32 on CPU. Never bfloat16."""
import wave

import numpy as np

from models import MODELS, ROOT, installed
from segments import normalize_segments, segments_from_probs

# The checkpoint's own offline profile: (340 + 40) * 80 ms = 30.4 s.
OFFLINE_CHUNK = 340
OFFLINE_RIGHT_CONTEXT = 40
OFFLINE_FIFO = 40
OFFLINE_CACHE_UPDATE = 300


class DiarizeError(Exception):
    pass


def load_wav(path):
    with wave.open(path, "rb") as handle:
        if handle.getnchannels() != 1 or handle.getframerate() != 16000 or handle.getsampwidth() != 2:
            raise DiarizeError("audio must be 16 kHz mono pcm_s16le")
        frames = handle.readframes(handle.getnframes())
    if not frames:
        return np.zeros(0, dtype=np.float32)
    return np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0


def diarize_path(path, model_name, device):
    if model_name not in MODELS:
        raise DiarizeError("unsupported model")
    device = "cuda" if device == "cuda" else "cpu"
    manifest = installed(model_name, verify=True)
    audio = load_wav(path)
    if audio.size == 0:
        return [], manifest["fingerprint"]
    import torch
    from transformers import AutoModelForAudioFrameClassification, AutoProcessor

    directory = str(ROOT / model_name)
    processor = AutoProcessor.from_pretrained(directory, local_files_only=True)
    dtype = torch.float16 if device == "cuda" else torch.float32
    model = AutoModelForAudioFrameClassification.from_pretrained(directory, local_files_only=True)
    if device == "cuda" and not torch.cuda.is_available():
        raise DiarizeError("cuda requested but this process has no CUDA device")
    model = model.to(device=device, dtype=dtype)
    model.eval()
    _require_offline(model)
    inputs = processor(audio, sampling_rate=16000)
    inputs = inputs.to(model.device, dtype=model.dtype)
    with torch.inference_mode():
        # No speaker_cache and no lookahead: the forward chunks the file itself.
        logits = model(**inputs).logits
    mask = inputs.get("attention_mask") if hasattr(inputs, "get") else None
    if hasattr(processor, "extract_speaker_dict"):
        raw = processor.extract_speaker_dict(logits, mask)[0]
        return normalize_segments(raw), manifest["fingerprint"]
    probs = logits.detach().float().sigmoid().cpu().tolist()
    return segments_from_probs(probs), manifest["fingerprint"]


def _require_offline(model):
    config = model.config
    got = (
        int(config.chunk_length),
        int(config.chunk_right_context),
        int(config.fifo_length),
        int(config.speaker_cache_update_period),
    )
    want = (OFFLINE_CHUNK, OFFLINE_RIGHT_CONTEXT, OFFLINE_FIFO, OFFLINE_CACHE_UPDATE)
    if got != want:
        raise DiarizeError(f"checkpoint offline profile {got} is not {want}")
