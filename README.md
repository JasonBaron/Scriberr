<div align="center">
  <img src="logo.svg" height="90" style="vertical-align: middle;" />
  <img src="logo-text.svg" height="80" style="vertical-align: middle;" />
</div>
</br>

<p align="center">
Self-hosted, offline audio transcription with speaker diarization, running on your own GPU.
</p>

<p align="center">
<b>This is a maintained fork of <a href="https://github.com/rishikanthc/Scriberr">rishikanthc/Scriberr</a>.</b><br/>
Upstream docs: <a href="https://github.com/rishikanthc/Scriberr#readme">original README</a> •
<a href="https://scriberr.app/docs/">scriberr.app/docs</a> •
<a href="https://scriberr.app/api">API reference</a>
</p>

<div align="center">
  <img src="screenshots/hero.png" alt="Scriberr" width="800" />
</div>

## About this fork

Scriberr is a solid self-hosted transcription app whose upstream development has been mostly idle. This fork keeps it running reliably on a home GPU server and adds the features its users have been asking for.

Goals, in order:

1. **Reliable.** Jobs should not break because an upstream tool released a new version, a container was recreated, or the network was down for a minute.
2. **Private.** Transcript text and secrets stay out of logs and process lists. Recordings never leave the box unless you configure a cloud model.
3. **Debuggable.** Every job writes a readable log: what ran, with which settings, how long each stage took, and why it failed.
4. **Useful.** Tags, AI-assisted tags and summaries, search, and progress estimates.

Every change is validated on a separate test stack (its own container, data and model environments) before it is merged to `main`. Changes land in numbered `fork-fixes-N` branches with one commit per fix.

Hardware it is tested on: RTX 3060 12 GB, i5-12600K, 62 GB RAM, `Dockerfile.cuda` (CUDA 12.6), Ubuntu host with Portainer.

## Status

### Upstream pull requests merged

| PR | Change |
|---|---|
| #482 | Validate `sortBy` / `sortOrder` before building SQL `ORDER BY` (injection fix) |
| #481 | Set the access-token cookie on registration so audio plays without logging in again |
| #463 | Fix the UID 1000 conflict with the `ubuntu` user in CUDA images on Ubuntu 24.04 |
| #474 | `SCRIBERR_ENABLED_MODELS` to skip installing models you do not use |
| #471 | Allow `large-v3-turbo` as a WhisperX model |
| #404 | Pipeline status endpoint, shown in the UI |
| #358 | Click a timestamp in Timeline view to seek |
| #480 | Copy transcript to clipboard |

### Fork fixes

| Branch | Change |
|---|---|
| fork-fixes-1 | Transcript text kept out of job logs (`SCRIBERR_LOG_TRANSCRIPTS=true` to keep it) |
| | Hugging Face token passed by environment, not on the command line where `ps` shows it |
| | `.m4a` and other audio extensions always treated as audio, including mobile uploads |
| | torchcodec pinned to 0.7 in the PyAnnote env (0.8+ needs CUDA 13 and breaks diarization); pins are re-applied to existing envs on startup |
| fork-fixes-2 | All fixable HIGH/CRITICAL CVEs patched: Go 1.26, current `golang.org/x` modules, `apt-get upgrade` in the image (134 to 0 in Trivy) |
| fork-fixes-3 | Structured job logs: header, pre-flight checks, numbered stages with timings, SUCCESS/FAILED footer with the real error and a hint |
| | uv pinned (0.12.23) and switched to `--system-certs` before the old `--native-tls` flag is removed |
| fork-fixes-4 | WhisperX pinned to a validated commit; an existing checkout is reused instead of failing on `git clone` |
| | Environment installs serialized per env (Parakeet and Canary share one and used to corrupt it) |
| | uv's Python and WhisperX's NLTK data stored on the env volume, so recreating the container does not break envs or re-download data |
| | Ollama `num_ctx` sized to the prompt, so long transcripts are no longer silently cut to their last few minutes |
| | Models not in `SCRIBERR_ENABLED_MODELS` hidden in the transcription dialog |
| | WhisperX's bundled VAD checkpoint upgraded once, removing a per-job Lightning warning |
| | Web UI and `/health` up within seconds of start; model checks run in the background and API writes return 503 until jobs can run. Docker `HEALTHCHECK` built in |
| | Entrypoint no longer re-chowns tens of GB of model envs on every start |
| | Time zone data embedded so `TZ` applies to log timestamps |

### Next

| Item | Notes |
|---|---|
| fork-fixes-5: YouTube | Transcripts do not appear after adding a video; Shorts links are not accepted |
| Tags | Manual tags with filtering in the list |
| AI tags and auto-summary | Suggested tags (accept or dismiss) and summaries on completion, through a local Ollama model |
| Progress and ETA | Per-model estimates from measured real-time factors |
| Metrics | Processing time, model and RTF per job in the UI |
| Transcript search | Search inside transcript text, not only titles |

## Build

The image is built locally from this repo. There is no published image for the fork.

```bash
git clone https://github.com/JasonBaron/Scriberr.git && cd Scriberr
docker build -f Dockerfile.cuda \
  --build-arg GIT_SHA=$(git rev-parse --short HEAD) \
  -t scriberr-cuda:main .
```

| Build arg | Default | Purpose |
|---|---|---|
| `GIT_SHA` | `dev` | Commit shown in every job log header |
| `UV_VERSION` | `0.12.23` | uv version installed in the image; bump deliberately |

`Dockerfile.cuda` targets CUDA 12.6 (GTX 10 series through RTX 40 series). `Dockerfile.cuda.12.9` is for RTX 50 series and gets the same changes, but is not tested here.

## Run (NVIDIA GPU)

Requires the [NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html).

```yaml
services:
  scriberr:
    image: scriberr-cuda:main
    ports:
      - "127.0.0.1:8080:8080"        # put a reverse proxy with TLS in front
    volumes:
      - ./data:/app/data             # database, uploads, transcripts, job logs
      - ./whisperx-env:/app/whisperx-env   # model envs, uv Python, NLTK data (large)
    restart: unless-stopped
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: all
              capabilities: [gpu]
    environment:
      - APP_ENV=production
      - PUID=1000
      - PGID=1000
      - TZ=America/New_York
      - HF_TOKEN=${HF_TOKEN}          # needed for PyAnnote diarization
      - SCRIBERR_ENABLED_MODELS=whisperx,pyannote
      - QUEUE_WORKERS=1               # one GPU job at a time
      - ALLOWED_ORIGINS=https://scriberr.example.com
```

Keep both volumes on local disk. uv's cache and venvs fail with permission errors on some union filesystems (mergerfs in particular).

The first start installs the model environments, which takes a while and downloads several GB. Later starts reuse them, but still check each one (about 30 to 90 s on a typical setup).

The web server starts immediately. While the environments are checked, `/health` returns `503 {"status":"starting","startup":"40s so far"}`, sign-in and browsing work, and uploads or new jobs return 503 with a retry hint. Once jobs can run, `/health` returns `200 {"status":"healthy","startup":"88s"}`. The image has a Docker `HEALTHCHECK` on `/health` (15 minute start period for first installs), so a compose `healthcheck:` block is not needed.

## Configuration

Upstream settings (`HOST`, `PORT`, `DATABASE_PATH`, `ALLOWED_ORIGINS`, `SECURE_COOKIES`, `JWT_SECRET`, `OPENAI_API_KEY` and others) work as documented in the [original README](https://github.com/rishikanthc/Scriberr#readme). Settings added or worth calling out in this fork:

| Variable | Default | Purpose |
|---|---|---|
| `SCRIBERR_ENABLED_MODELS` | all | Models to install at startup; others are hidden in the UI. IDs: `whisperx`, `pyannote`, `parakeet`, `canary`, `sortformer`, `voxtral`, `openai_whisper` |
| `QUEUE_WORKERS` | 2 to 6 | Jobs run in parallel. Use `1` on a single consumer GPU |
| `HF_TOKEN` | | Hugging Face token for PyAnnote models (accept their terms on huggingface.co) |
| `SCRIBERR_LOG_TRANSCRIPTS` | `false` | Keep transcript text in job logs |
| `SCRIBERR_LOG_VERBOSE` | `false` | Raw model output in job logs (no warning filtering or indentation) |
| `SCRIBERR_WHISPERX_REF` | pinned commit | WhisperX commit, tag or branch for new installs |
| `OLLAMA_NUM_CTX` | auto | Fixed Ollama context window for every request |
| `OLLAMA_NUM_CTX_MAX` | `32768` | Cap on the automatic context size (KV cache uses GPU memory) |
| `UV_PYTHON_INSTALL_DIR` | `/app/whisperx-env/.uv-python` | Where uv installs Python (set in the image) |
| `NLTK_DATA` | `/app/whisperx-env/.nltk_data` | WhisperX alignment tokenizer data (set in the image) |
| `TZ` | UTC | Time zone for job log timestamps |
| `SCRIBERR_FIX_OWNERSHIP` | `false` | Force a full `chown` of `/app/data` and `/app/whisperx-env` on start (otherwise only when the top-level owner is wrong) |

For summaries and chat with Ollama, use Scriberr's **Ollama** provider. The OpenAI-compatible provider cannot pass `num_ctx`, so long transcripts get truncated there.

## Job logs

**View Logs** on any job shows a log like this:

```
======================================
SCRIBERR JOB START
======================================
Job ID      : 43188d01-c870-440a-bd71-af26ab6530c1
Title       : Team sync
Audio       : 43188d01-....wav (9.2 MB)
Started     : 2026-10-07 10:43:56 EDT
Version     : 5da8cd7
======================================
PRE-FLIGHT
--------------------------------------
GPU         : NVIDIA GeForce RTX 3060, 11.4 GB free of 12.0 GB
uv          : uv 0.12.23 (x86_64-unknown-linux-gnu)
HF token    : present (job parameters)
Profile     : whisper large-v3 | cuda float16 | batch 4 | en | VAD pyannote 0.50/0.363 | diarize pyannote (2-2 speakers)
[1/3] Preparing audio...
      done  0.2s  (300.0 s, wav, no conversion needed)
[2/3] Transcribing + diarizing (whisperx large-v3)...
      - voice activity detection using Pyannote (+39.3s)
      - transcription (+40.5s)
      - alignment (+51.5s)
      - diarization (+53.7s)
      [scriberr] 10 transcript line(s) omitted from this log
      [scriberr] 3 known warning(s) suppressed (TF32, hf_token flag, pyannote std())
      done  66.5s  (62 segments, 432 words, 2 speakers)
[3/3] Saving transcript...
      done  0.0s  (saved to database)
======================================
SCRIBERR JOB COMPLETE
======================================
Duration    : 66.7 s (RTF 0.222)
Status      : SUCCESS
======================================
```

On failure the footer names the stage, shows the actual Python error instead of a wrapped one, and adds a hint for known causes (torchcodec mismatch, CUDA out of memory, missing Hugging Face token, cuDNN not found). The token value and transcript text are never written.

## Model notes (RTX 3060 12 GB)

Measured on a 68-minute, two-speaker recording with PyAnnote diarization, end to end through the API:

| Model | Time | Notes |
|---|---|---|
| WhisperX large-v3 (float16, batch 4) | 263 s | Default. Best accuracy |
| WhisperX large-v3-turbo | 223 s | About a dozen fewer words than large-v3 |
| WhisperX medium.en | 229 s | |
| WhisperX small / small.en | ~201 s | Repetition loops seen |
| WhisperX base.en | 191 s | |
| Parakeet tdt-0.6b-v3 + PyAnnote community-1 | 347 s | Fast transcription (111 s), slower separate diarization |
| Canary 1b-v2 | | No chunking; too slow for long files |

About 185 s of every WhisperX run is fixed cost (model loading and diarization), so smaller models save less than expected.

Recommended profile: `large-v3`, `cuda`, `float16`, batch size 4, language set explicitly, PyAnnote VAD onset 0.5 / offset 0.363, PyAnnote diarization with min and max speakers set when known. Lowering VAD onset to 0.3 or switching to Silero recovered some muffled speech but dropped more elsewhere.

## Credits and license

Scriberr was created by [Rishikanth Chandrasekaran](https://github.com/rishikanthc) and its contributors; the upstream pull requests listed above are their work. If Scriberr is useful to you, consider [supporting the original author](https://ko-fi.com/H2H41KQZA3).

MIT License, same as upstream. See [LICENSE](LICENSE).
