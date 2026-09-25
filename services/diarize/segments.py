"""Turn per-frame speaker probabilities into arrival-order segments.

Eight channels. Overlap stays as two segments. A ninth column is ignored.
"""

FRAME_SEC = 0.01
THRESHOLD = 0.5
MAX_SPEAKERS = 8


def segments_from_probs(probs, frame_sec=FRAME_SEC, threshold=THRESHOLD):
    """probs is a sequence of frames, each a sequence of speaker probabilities."""
    if not probs:
        return []
    width = min(MAX_SPEAKERS, len(probs[0]))
    segments = []
    for channel in range(width):
        start = None
        for index, row in enumerate(probs):
            active = float(row[channel]) >= threshold
            if active and start is None:
                start = index
            elif not active and start is not None:
                segments.append(_segment(channel, start, index, frame_sec))
                start = None
        if start is not None:
            segments.append(_segment(channel, start, len(probs), frame_sec))
    segments.sort(key=lambda item: (item["start"], item["speaker"]))
    return segments


def normalize_segments(raw):
    """Accept processor dicts (Start/End/Speaker) and drop anything past speaker 7."""
    segments = []
    for item in raw or []:
        speaker = item.get("Speaker", item.get("speaker"))
        start = float(item.get("Start", item.get("start")))
        end = float(item.get("End", item.get("end")))
        index = _speaker_index(speaker)
        if index is None or index < 0 or index >= MAX_SPEAKERS or end <= start:
            continue
        segments.append(
            {
                "start": round(start, 3),
                "end": round(end, 3),
                "speaker": f"speaker_{index}",
            }
        )
    segments.sort(key=lambda item: (item["start"], item["speaker"]))
    return segments


def _speaker_index(speaker):
    if isinstance(speaker, str):
        if not speaker.startswith("speaker_"):
            return None
        speaker = speaker.split("_", 1)[1]
    try:
        return int(speaker)
    except (TypeError, ValueError):
        return None


def _segment(channel, start_frame, end_frame, frame_sec):
    return {
        "start": round(start_frame * frame_sec, 3),
        "end": round(end_frame * frame_sec, 3),
        "speaker": f"speaker_{channel}",
    }
