"""Filesystem bounds for audio the sidecar is willing to read."""
import os
import tempfile
from pathlib import Path


def audio_roots():
    roots = [Path(tempfile.gettempdir()).resolve()]
    extra = os.environ.get("DIARIZE_AUDIO_ROOT", "").strip()
    if extra:
        roots.append(Path(extra).resolve())
    return roots


def allowed_audio(path):
    try:
        resolved = Path(path).resolve()
    except OSError:
        return False
    if not resolved.is_file():
        return False
    for root in audio_roots():
        if resolved == root or root in resolved.parents:
            return True
    return False
