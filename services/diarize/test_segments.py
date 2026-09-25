import tempfile
import unittest
from pathlib import Path

from paths import allowed_audio
from segments import normalize_segments, segments_from_probs


class SegmentsTest(unittest.TestCase):
    def test_merges_a_run_and_keeps_overlap(self):
        silence = [0.0, 0.0]
        both = [0.9, 0.8]
        probs = [silence, both, both, [0.9, 0.1], silence]
        got = segments_from_probs(probs, frame_sec=0.01, threshold=0.5)
        self.assertEqual(
            got,
            [
                {"start": 0.01, "end": 0.04, "speaker": "speaker_0"},
                {"start": 0.01, "end": 0.03, "speaker": "speaker_1"},
            ],
        )

    def test_threshold_and_ninth_channel(self):
        frame = [0.49, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 1.0]
        self.assertEqual(segments_from_probs([frame, frame]), [])
        hot = [0.5, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 1.0]
        got = segments_from_probs([hot])
        self.assertEqual(got, [{"start": 0.0, "end": 0.01, "speaker": "speaker_0"}])

    def test_normalize_drops_speaker_eight(self):
        raw = [
            {"Start": 1.0, "End": 2.0, "Speaker": 8},
            {"Start": 0.0, "End": 1.5, "Speaker": 1},
            {"start": 1.2, "end": 1.2, "speaker": "speaker_0"},
        ]
        self.assertEqual(
            normalize_segments(raw),
            [{"start": 0.0, "end": 1.5, "speaker": "speaker_1"}],
        )

    def test_audio_path_stays_in_temp(self):
        self.assertFalse(allowed_audio(__file__))
        handle = tempfile.NamedTemporaryFile(suffix=".wav", delete=False)
        handle.close()
        try:
            self.assertTrue(allowed_audio(handle.name))
            outside = Path(tempfile.gettempdir()).parent / "rewind-diarize-not-temp.wav"
            self.assertFalse(allowed_audio(str(outside)))
        finally:
            Path(handle.name).unlink(missing_ok=True)


if __name__ == "__main__":
    unittest.main()
