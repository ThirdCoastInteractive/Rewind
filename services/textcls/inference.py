"""ONNX sequence classification; returns zeros when weights are missing (canary)."""
import json
from pathlib import Path

import numpy as np
import onnxruntime as ort
from tokenizers import Tokenizer

from models import ROOT, MODELS, installed, ModelUnavailable


def softmax(logits):
    logits = np.asarray(logits, dtype=np.float64)
    logits = logits - np.max(logits)
    exp = np.exp(logits)
    return exp / np.sum(exp)


def sigmoid(logits):
    logits = np.asarray(logits, dtype=np.float64)
    return 1.0 / (1.0 + np.exp(-logits))


class Engine:
    def __init__(self, device="cpu"):
        self.device = device
        if device == "cuda" and "CUDAExecutionProvider" not in ort.get_available_providers():
            raise RuntimeError("CUDA provider unavailable")
        self.providers = ["CUDAExecutionProvider", "CPUExecutionProvider"] if device == "cuda" else ["CPUExecutionProvider"]
        self.sessions = {}
        self.tokenizers = {}
        self.configs = {}

    def session(self, name):
        if name not in self.sessions:
            installed(name)
            opts = ort.SessionOptions()
            opts.intra_op_num_threads = 2
            opts.inter_op_num_threads = 1
            path = ROOT / name / "model.onnx"
            session = ort.InferenceSession(str(path), sess_options=opts, providers=self.providers)
            if self.device == "cuda" and session.get_providers()[0] != "CUDAExecutionProvider":
                raise RuntimeError("CUDA session unexpectedly fell back to CPU")
            self.sessions[name] = session
            self.configs[name] = json.loads((ROOT / name / "config.json").read_text())
            tok = Tokenizer.from_file(str(ROOT / name / "tokenizer.json"))
            tok.enable_padding(length=128, pad_id=1, pad_token="<pad>")
            tok.enable_truncation(max_length=128)
            self.tokenizers[name] = tok
        return self.sessions[name]

    def encode(self, name, text):
        self.session(name)
        encoded = self.tokenizers[name].encode((text or "").strip() or " ")
        ids = np.array([encoded.ids], dtype=np.int64)
        mask = np.array([encoded.attention_mask], dtype=np.int64)
        return ids, mask

    def run_logits(self, name, text):
        session = self.session(name)
        ids, mask = self.encode(name, text)
        feeds = {}
        for inp in session.get_inputs():
            key = inp.name.lower()
            if "mask" in key:
                feeds[inp.name] = mask.astype(np.int64 if "int64" in inp.type else np.int32)
            else:
                feeds[inp.name] = ids.astype(np.int64 if "int64" in inp.type else np.int32)
        outputs = session.run(None, feeds)
        return np.asarray(outputs[0][0], dtype=np.float64)

    def sentiment(self, name, text):
        logits = self.run_logits(name, text)
        probs = softmax(logits)
        cfg = self.configs[name]
        labels = {str(cfg["id2label"][str(i)]): float(probs[i]) for i in range(len(probs))}
        # Map neg/neu/pos probabilities onto [-1, 1].
        score = float(labels.get("positive", 0) - labels.get("negative", 0))
        return max(-1.0, min(1.0, score)), labels

    def toxicity(self, name, text):
        logits = self.run_logits(name, text)
        probs = sigmoid(logits)
        cfg = self.configs[name]
        labels = {str(cfg["id2label"][str(i)]): float(probs[i]) for i in range(len(probs))}
        return float(labels.get("toxicity", float(np.max(probs)))), labels

    def classify(self, text, sentiment_model, toxicity_model):
        sentiment, sent_labels = self.sentiment(sentiment_model, text)
        toxicity, tox_labels = self.toxicity(toxicity_model, text)
        labels = {"sentiment": sent_labels, "toxicity": tox_labels}
        return {"sentiment": sentiment, "toxicity": toxicity, "labels": labels}


def stub_item(item_id):
    return {"id": item_id, "sentiment": 0.0, "toxicity": 0.0, "labels": {}}
