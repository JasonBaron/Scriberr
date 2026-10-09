#!/usr/bin/env python3
"""
Voice prints for the speakers of one recording.

Reads the recording and a JSON list of speaker turns (from the saved
transcript), and writes one embedding per speaker: the duration-weighted
mean of embeddings taken from that speaker's turns. Each chunk's similarity
to its speaker's mean is reported too, so turns that sound like someone else
(two people under one label) can be spotted.

Input JSON:  {"segments": [{"speaker": "SPEAKER_00", "start": 1.2, "end": 4.8}, ...]}
Output JSON: {"model": "...", "dim": 256, "speakers": {"SPEAKER_00": {
                "embedding": [...], "seconds": 312.4, "chunks": [[start, end, score], ...]}}}
"""

import argparse
import json
import os
import sys

import numpy as np
import torch

try:
    from pyannote.audio.core.task import Specifications, Problem, Resolution
    if hasattr(torch.serialization, "add_safe_globals"):
        torch.serialization.add_safe_globals([Specifications, Problem, Resolution])
except Exception:
    pass

from pyannote.audio import Inference, Model
from pyannote.audio.core.io import Audio
from pyannote.core import Segment

SAMPLE_RATE = 16000

# Embedding models to try, in order. The first is the one inside the
# diarization pipeline Scriberr already uses (license already accepted).
MODELS = [
    ("pyannote/speaker-diarization-community-1", "embedding"),
    ("pyannote/wespeaker-voxceleb-resnet34-LM", None),
]


def load_model(token, wanted=None):
    errors = []
    candidates = [(wanted, None)] if wanted else MODELS
    for name, subfolder in candidates:
        try:
            if subfolder:
                model = Model.from_pretrained(name, subfolder=subfolder, token=token)
            else:
                model = Model.from_pretrained(name, token=token)
            if model is not None:
                label = f"{name}/{subfolder}" if subfolder else name
                return model, label
        except Exception as e:  # try the next one
            errors.append(f"{name}: {e}")
    raise RuntimeError("Could not load a speaker embedding model. " + " | ".join(errors))


def merge_turns(segments, gap):
    """Joins a speaker's back-to-back turns (gap seconds apart or less)."""
    by_speaker = {}
    for s in sorted(segments, key=lambda s: (s["speaker"], s["start"])):
        turns = by_speaker.setdefault(s["speaker"], [])
        if turns and s["start"] - turns[-1][1] <= gap:
            turns[-1][1] = max(turns[-1][1], s["end"])
        else:
            turns.append([s["start"], s["end"]])
    return by_speaker


def chunks_for(turns, min_len, max_len, trim, limit):
    out = []
    for start, end in turns:
        start, end = start + trim, end - trim
        while end - start >= min_len:
            stop = min(end, start + max_len)
            if stop - start >= min_len:
                out.append((start, stop))
            start = stop
    # Longest chunks first; keep them in time order afterwards
    out.sort(key=lambda c: c[1] - c[0], reverse=True)
    return sorted(out[:limit])


def unit(v):
    n = np.linalg.norm(v)
    return v / n if n > 0 else v


def main():
    p = argparse.ArgumentParser(description="Speaker voice prints for one recording")
    p.add_argument("audio")
    p.add_argument("--segments", required=True)
    p.add_argument("--output", required=True)
    p.add_argument("--model", default=None)
    p.add_argument("--min-chunk", type=float, default=1.5)
    p.add_argument("--max-chunk", type=float, default=8.0)
    p.add_argument("--max-chunks", type=int, default=40)
    args = p.parse_args()

    token = os.environ.get("HF_TOKEN")
    if not token:
        print("Error: HF_TOKEN is not set")
        sys.exit(1)

    with open(args.segments) as f:
        segments = json.load(f).get("segments", [])
    segments = [s for s in segments if s.get("speaker") and s["end"] > s["start"]]
    if not segments:
        print("No speaker turns in the transcript")
        with open(args.output, "w") as f:
            json.dump({"model": "", "dim": 0, "speakers": {}}, f)
        return

    model, label = load_model(token, args.model)
    device = torch.device("cuda" if torch.cuda.is_available() else "cpu")
    inference = Inference(model, window="whole")
    inference.to(device)
    print(f"Embedding model: {label} on {device}")

    waveform, sr = Audio(sample_rate=SAMPLE_RATE, mono="downmix")(args.audio)
    audio = {"waveform": waveform, "sample_rate": sr}
    duration = waveform.shape[-1] / sr
    print(f"Audio: {duration:.1f}s, {len(segments)} turns")

    result = {"model": label, "dim": 0, "speakers": {}}
    for speaker, turns in merge_turns(segments, gap=0.3).items():
        chunks = chunks_for(turns, args.min_chunk, args.max_chunk, trim=0.1, limit=args.max_chunks)
        chunks = [(s, min(e, duration)) for s, e in chunks if s < duration and min(e, duration) - s >= args.min_chunk]
        if not chunks:
            continue
        vecs, weights = [], []
        for s, e in chunks:
            emb = np.asarray(inference.crop(audio, Segment(s, e)), dtype=np.float64).reshape(-1)
            if not np.all(np.isfinite(emb)):
                continue
            vecs.append(unit(emb))
            weights.append(e - s)
        if not vecs:
            continue
        vecs = np.stack(vecs)
        w = np.asarray(weights)
        centroid = unit((vecs * w[:, None]).sum(axis=0))
        scores = vecs @ centroid
        result["dim"] = int(centroid.shape[0])
        result["speakers"][speaker] = {
            "embedding": [round(float(x), 6) for x in centroid],
            "seconds": round(float(sum(e - s for s, e in turns)), 2),
            "chunks": [[round(s, 2), round(e, 2), round(float(sc), 4)] for (s, e), sc in zip(chunks, scores)],
        }
        print(f"  {speaker}: {len(vecs)} chunks, mean similarity {float((scores * w).sum() / w.sum()):.2f}")

    with open(args.output, "w") as f:
        json.dump(result, f)
    print(f"Voice prints saved: {len(result['speakers'])} speakers")


if __name__ == "__main__":
    main()
