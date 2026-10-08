package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"scriberr/internal/transcription/interfaces"
	"scriberr/pkg/logger"
)

// WhisperXAdapter implements the TranscriptionAdapter interface for WhisperX
type WhisperXAdapter struct {
	*BaseAdapter
	envPath string
}

// NewWhisperXAdapter creates a new WhisperX adapter
func NewWhisperXAdapter(envPath string) *WhisperXAdapter {
	capabilities := interfaces.ModelCapabilities{
		ModelID:     "whisperx",
		ModelFamily: "whisper",
		DisplayName: "WhisperX",
		Description: "OpenAI Whisper with speaker diarization and word-level timestamps",
		Version:     "3.0.0",
		SupportedLanguages: []string{
			"en", "zh", "de", "es", "ru", "ko", "fr", "ja", "pt", "tr", "pl", "ca", "nl",
			"ar", "sv", "it", "id", "hi", "fi", "vi", "he", "uk", "el", "ms", "cs", "ro",
			"da", "hu", "ta", "no", "th", "ur", "hr", "bg", "lt", "la", "mi", "ml", "cy",
			"sk", "te", "fa", "lv", "bn", "sr", "az", "sl", "kn", "et", "mk", "br", "eu",
			"is", "hy", "ne", "mn", "bs", "kk", "sq", "sw", "gl", "mr", "pa", "si", "km",
			"sn", "yo", "so", "af", "oc", "ka", "be", "tg", "sd", "gu", "am", "yi", "lo",
			"uz", "fo", "ht", "ps", "tk", "nn", "mt", "sa", "lb", "my", "bo", "tl", "mg",
			"as", "tt", "haw", "ln", "ha", "ba", "jw", "su", "auto",
		},
		SupportedFormats:  []string{"wav", "mp3", "flac", "m4a", "ogg", "wma"},
		RequiresGPU:       false, // Optional GPU support
		MemoryRequirement: 2048,  // 2GB base requirement
		Features: map[string]bool{
			"timestamps":         true,
			"word_level":         true,
			"diarization":        true,
			"translation":        true,
			"language_detection": true,
			"vad":                true,
		},
		Metadata: map[string]string{
			"engine":     "openai_whisper",
			"framework":  "transformers",
			"license":    "MIT",
			"python_env": "whisperx",
		},
	}

	schema := []interfaces.ParameterSchema{
		// Model selection
		{
			Name:        "model",
			Type:        "string",
			Required:    false,
			Default:     "small",
			Options:     []string{"tiny", "tiny.en", "base", "base.en", "small", "small.en", "medium", "medium.en", "large", "large-v1", "large-v2", "large-v3", "large-v3-turbo"},
			Description: "Whisper model size to use",
			Group:       "basic",
		},

		// Device and computation
		{
			Name:        "device",
			Type:        "string",
			Required:    false,
			Default:     "cpu",
			Options:     []string{"cpu", "cuda"},
			Description: "Device to use for computation",
			Group:       "basic",
		},
		{
			Name:        "device_index",
			Type:        "int",
			Required:    false,
			Default:     0,
			Min:         &[]float64{0}[0],
			Max:         &[]float64{7}[0],
			Description: "GPU device index to use",
			Group:       "advanced",
		},
		{
			Name:        "batch_size",
			Type:        "int",
			Required:    false,
			Default:     8,
			Min:         &[]float64{1}[0],
			Max:         &[]float64{64}[0],
			Description: "Batch size for processing",
			Group:       "advanced",
		},
		{
			Name:        "compute_type",
			Type:        "string",
			Required:    false,
			Default:     "float32",
			Options:     []string{"float16", "float32", "int8"},
			Description: "Computation precision",
			Group:       "advanced",
		},
		{
			Name:        "threads",
			Type:        "int",
			Required:    false,
			Default:     0,
			Min:         &[]float64{0}[0],
			Max:         &[]float64{32}[0],
			Description: "Number of CPU threads (0 = auto)",
			Group:       "advanced",
		},

		// Language and task
		{
			Name:        "language",
			Type:        "string",
			Required:    false,
			Default:     nil,
			Description: "Language code (auto-detect if not specified)",
			Group:       "basic",
		},
		{
			Name:        "task",
			Type:        "string",
			Required:    false,
			Default:     "transcribe",
			Options:     []string{"transcribe", "translate"},
			Description: "Task to perform",
			Group:       "basic",
		},

		// Diarization
		{
			Name:        "diarize",
			Type:        "bool",
			Required:    false,
			Default:     false,
			Description: "Enable speaker diarization",
			Group:       "basic",
		},
		{
			Name:        "diarize_model",
			Type:        "string",
			Required:    false,
			Default:     "pyannote/speaker-diarization-3.1",
			Options:     []string{"pyannote/speaker-diarization-3.1", "pyannote"},
			Description: "Diarization model to use",
			Group:       "advanced",
		},
		{
			Name:        "min_speakers",
			Type:        "int",
			Required:    false,
			Default:     nil,
			Min:         &[]float64{1}[0],
			Max:         &[]float64{20}[0],
			Description: "Minimum number of speakers",
			Group:       "advanced",
		},
		{
			Name:        "max_speakers",
			Type:        "int",
			Required:    false,
			Default:     nil,
			Min:         &[]float64{1}[0],
			Max:         &[]float64{20}[0],
			Description: "Maximum number of speakers",
			Group:       "advanced",
		},
		{
			Name:        "hf_token",
			Type:        "string",
			Required:    false,
			Default:     nil,
			Description: "HuggingFace token for diarization models (optional if HF_TOKEN env var is set)",
			Group:       "advanced",
		},

		// Quality settings
		{
			Name:        "temperature",
			Type:        "float",
			Required:    false,
			Default:     0.0,
			Min:         &[]float64{0.0}[0],
			Max:         &[]float64{1.0}[0],
			Description: "Sampling temperature",
			Group:       "quality",
		},
		{
			Name:        "best_of",
			Type:        "int",
			Required:    false,
			Default:     5,
			Min:         &[]float64{1}[0],
			Max:         &[]float64{10}[0],
			Description: "Number of candidates to consider",
			Group:       "quality",
		},
		{
			Name:        "beam_size",
			Type:        "int",
			Required:    false,
			Default:     5,
			Min:         &[]float64{1}[0],
			Max:         &[]float64{10}[0],
			Description: "Beam search size",
			Group:       "quality",
		},
		{
			Name:        "patience",
			Type:        "float",
			Required:    false,
			Default:     1.0,
			Min:         &[]float64{0.0}[0],
			Max:         &[]float64{2.0}[0],
			Description: "Beam search patience",
			Group:       "quality",
		},

		// VAD settings
		{
			Name:        "vad_method",
			Type:        "string",
			Required:    false,
			Default:     "pyannote",
			Options:     []string{"pyannote", "silero"},
			Description: "Voice activity detection method",
			Group:       "advanced",
		},
		{
			Name:        "vad_onset",
			Type:        "float",
			Required:    false,
			Default:     0.5,
			Min:         &[]float64{0.0}[0],
			Max:         &[]float64{1.0}[0],
			Description: "VAD onset threshold",
			Group:       "advanced",
		},
		{
			Name:        "vad_offset",
			Type:        "float",
			Required:    false,
			Default:     0.363,
			Min:         &[]float64{0.0}[0],
			Max:         &[]float64{1.0}[0],
			Description: "VAD offset threshold",
			Group:       "advanced",
		},

		// Custom Alignment Model
		{
			Name:        "align_model",
			Type:        "string",
			Required:    false,
			Default:     nil,
			Description: "Custom alignment model (e.g. KBLab/wav2vec2-large-voxrex-swedish)",
			Group:       "advanced",
		},
	}

	baseAdapter := NewBaseAdapter("whisperx", filepath.Join(envPath, "WhisperX"), capabilities, schema)

	adapter := &WhisperXAdapter{
		BaseAdapter: baseAdapter,
		envPath:     envPath,
	}

	return adapter
}

// GetSupportedModels returns the list of Whisper models supported
func (w *WhisperXAdapter) GetSupportedModels() []string {
	return []string{
		"tiny", "tiny.en",
		"base", "base.en",
		"small", "small.en",
		"medium", "medium.en",
		"large", "large-v1", "large-v2", "large-v3", "large-v3-turbo",
	}
}

// PrepareEnvironment sets up the WhisperX environment
func (w *WhisperXAdapter) PrepareEnvironment(ctx context.Context) error {
	defer lockEnv(w.envPath)()

	logger.Info("Preparing WhisperX environment", "env_path", w.envPath)

	ensureNLTKDataDir()

	whisperxPath := filepath.Join(w.envPath, "WhisperX")

	// Check if WhisperX is already set up and working (using cache to speed up repeated checks)
	if CheckEnvironmentReady(whisperxPath, "import whisperx") {
		// A working env is left on whatever commit it has; report drift from the pin.
		if head := gitHead(whisperxPath); head != "" && !strings.HasPrefix(head, whisperXRef()) && !strings.HasPrefix(whisperXRef(), head) {
			logger.Warn("WhisperX checkout differs from the pinned commit; delete the WhisperX folder to reinstall at the pin",
				"current", head, "pinned", whisperXRef())
		}
		w.migrateVADCheckpoint(whisperxPath)
		logger.Info("WhisperX environment already ready")
		w.initialized = true
		return nil
	}

	// Ensure base directory exists
	if err := os.MkdirAll(w.envPath, 0755); err != nil {
		return fmt.Errorf("failed to create environment directory: %w", err)
	}

	// Clone WhisperX, or reuse an existing checkout, at the pinned commit
	if err := w.ensureWhisperXCheckout(whisperxPath); err != nil {
		return fmt.Errorf("failed to prepare WhisperX source: %w", err)
	}

	// Update dependencies
	if err := w.updateWhisperXDependencies(whisperxPath); err != nil {
		return fmt.Errorf("failed to update WhisperX dependencies: %w", err)
	}

	// Install dependencies
	if err := w.uvSyncWhisperX(whisperxPath); err != nil {
		return fmt.Errorf("failed to sync WhisperX: %w", err)
	}

	w.migrateVADCheckpoint(whisperxPath)

	w.initialized = true
	logger.Info("WhisperX environment prepared successfully")
	return nil
}

const (
	// DefaultWhisperXRef is the WhisperX commit this build is validated against
	// (m-bain/WhisperX main, 2026-09-26). Override with SCRIBERR_WHISPERX_REF.
	DefaultWhisperXRef = "771b4a14a9486f8fd5aef18ef49e35d639523dd3"
	// EnvWhisperXRef overrides the WhisperX commit, tag or branch to install.
	EnvWhisperXRef = "SCRIBERR_WHISPERX_REF"

	whisperXRepoURL = "https://github.com/m-bain/WhisperX.git"
	// vadCheckpointMarker records that the bundled VAD checkpoint was upgraded.
	vadCheckpointMarker = ".scriberr-vad-checkpoint-migrated"
)

func whisperXRef() string {
	if v := strings.TrimSpace(os.Getenv(EnvWhisperXRef)); v != "" {
		return v
	}
	return DefaultWhisperXRef
}

// gitHead returns the checked-out commit of dir, or "" if it is not a git checkout.
func gitHead(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-c", "advice.detachedHead=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ensureWhisperXCheckout puts the WhisperX source at the pinned ref. An existing
// checkout is reused (a plain "git clone" fails when the folder already exists,
// which left half-installed envs unrecoverable).
func (w *WhisperXAdapter) ensureWhisperXCheckout(whisperxPath string) error {
	ref := whisperXRef()

	if _, err := os.Stat(filepath.Join(whisperxPath, ".git")); err == nil {
		logger.Info("Reusing existing WhisperX checkout", "path", whisperxPath, "ref", ref)
		// Fetch can fail offline; checkout still works if the ref is already present.
		if err := runGit(whisperxPath, "fetch", "--quiet", "origin"); err != nil {
			logger.Warn("WhisperX fetch failed, trying local checkout", "error", err)
		}
	} else if _, err := os.Stat(whisperxPath); err == nil {
		return fmt.Errorf("%s exists but is not a git checkout; move it aside and restart", whisperxPath)
	} else {
		logger.Info("Cloning WhisperX", "ref", ref)
		if err := runGit(w.envPath, "clone", "--quiet", whisperXRepoURL, filepath.Base(whisperxPath)); err != nil {
			return err
		}
	}

	// -f discards Scriberr's earlier pyproject edits; they are re-applied next.
	if err := runGit(whisperxPath, "checkout", "--quiet", "-f", ref); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(whisperxPath, vadCheckpointMarker)) // checkout restored the original file
	return nil
}

// ensureNLTKDataDir creates $NLTK_DATA if set. NLTK only downloads into an
// existing, writable directory on its search path and otherwise falls back to
// ~/nltk_data in the container layer, which is lost on every recreate. WhisperX
// downloads punkt_tab during alignment and fails the job if that download fails.
func ensureNLTKDataDir() {
	dir := strings.TrimSpace(os.Getenv("NLTK_DATA"))
	if dir == "" {
		return
	}
	// NLTK_DATA may list several paths; the first is where downloads go.
	if i := strings.IndexByte(dir, os.PathListSeparator); i >= 0 {
		dir = dir[:i]
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		logger.Warn("Could not create NLTK data directory", "path", dir, "error", err)
	}
}

// vadMigrationScript upgrades WhisperX's bundled VAD checkpoint (saved with
// Lightning 1.5.4) so it is not re-upgraded in memory, with a warning, on every job.
// The file ships with WhisperX and is already fully loaded by it on each run.
const vadMigrationScript = `
import torch
from lightning.pytorch.utilities.migration import migrate_checkpoint
p = "whisperx/assets/pytorch_model.bin"
ck = torch.load(p, map_location="cpu", weights_only=False)
ck, applied = migrate_checkpoint(ck)
if applied:
    torch.save(ck, p)
print("migrations applied:", list(applied) or "none")
`

// migrateVADCheckpoint runs once per checkout (tracked by a marker file).
// Failure only leaves the cosmetic warning in place, so it never fails setup.
func (w *WhisperXAdapter) migrateVADCheckpoint(whisperxPath string) {
	marker := filepath.Join(whisperxPath, vadCheckpointMarker)
	if _, err := os.Stat(marker); err == nil {
		return
	}
	if _, err := os.Stat(filepath.Join(whisperxPath, "whisperx", "assets", "pytorch_model.bin")); err != nil {
		return
	}
	cmd := exec.Command("uv", "run", UVTLSFlag, "--project", whisperxPath, "python", "-c", vadMigrationScript)
	cmd.Dir = whisperxPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Warn("WhisperX VAD checkpoint upgrade failed (harmless; Lightning warning stays)",
			"error", err, "output", strings.TrimSpace(string(out)))
		return
	}
	logger.Info("WhisperX VAD checkpoint upgraded", "result", strings.TrimSpace(string(out)))
	_ = os.WriteFile(marker, []byte(time.Now().Format(time.RFC3339)+"\n"), 0644)
}

// updateWhisperXDependencies modifies WhisperX pyproject.toml
func (w *WhisperXAdapter) updateWhisperXDependencies(whisperxPath string) error {
	pyprojectPath := filepath.Join(whisperxPath, "pyproject.toml")

	data, err := os.ReadFile(pyprojectPath)
	if err != nil {
		return fmt.Errorf("failed to read pyproject.toml: %w", err)
	}

	content := string(data)
	content = strings.ReplaceAll(content, "ctranslate2<4.5.0", "ctranslate2==4.6.0")

	// torchcodec>=0.6.0 (upstream default) resolves to 0.10.0+ which requires PyTorch 2.9.
	// Pin to 0.7.x which is compatible with the PyTorch 2.8.x used here.
	content = strings.ReplaceAll(content, "torchcodec>=0.6.0", "torchcodec~=0.7.0")

	if !strings.Contains(content, "yt-dlp") {
		content = strings.ReplaceAll(content,
			`"transformers>=4.48.0",`,
			`"transformers>=4.48.0",
    "yt-dlp[default]",`)
	}

	// Set PyTorch CUDA version based on environment configuration
	// The repo already has the correct [tool.uv.sources] configuration, we just need to update the CUDA version
	// This allows using cu126 for legacy GPUs (GTX 10-series through RTX 40-series) or cu128 for Blackwell (RTX 50-series)
	content = strings.ReplaceAll(content, "https://download.pytorch.org/whl/cu128", GetPyTorchWheelURL())

	if err := os.WriteFile(pyprojectPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write pyproject.toml: %w", err)
	}

	return nil
}

// uvSyncWhisperX runs uv sync for WhisperX
func (w *WhisperXAdapter) uvSyncWhisperX(whisperxPath string) error {
	cmd := exec.Command("uv", "sync", "--all-extras", "--dev", UVTLSFlag)
	cmd.Dir = whisperxPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("uv sync failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Transcribe processes audio using WhisperX
func (w *WhisperXAdapter) Transcribe(ctx context.Context, input interfaces.AudioInput, params map[string]interface{}, procCtx interfaces.ProcessingContext) (*interfaces.TranscriptResult, error) {
	startTime := time.Now()
	w.LogProcessingStart(input, procCtx)
	defer func() {
		w.LogProcessingEnd(procCtx, time.Since(startTime), nil)
	}()

	// Validate input
	if err := w.ValidateAudioInput(input); err != nil {
		return nil, fmt.Errorf("invalid audio input: %w", err)
	}

	// Validate parameters
	if err := w.ValidateParameters(params); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	// Create temporary directory
	tempDir, err := w.CreateTempDirectory(procCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer w.CleanupTempDirectory(tempDir)

	// Build WhisperX command
	args, err := w.buildWhisperXArgs(input, params, tempDir)
	if err != nil {
		return nil, fmt.Errorf("failed to build command: %w", err)
	}

	// Execute WhisperX
	cmd := exec.CommandContext(ctx, "uv", args...)

	// Add nvidia libraries to LD_LIBRARY_PATH
	env := os.Environ()
	if nvidiaPaths, err := w.findNvidiaLibPaths(); err == nil && len(nvidiaPaths) > 0 {
		ldLibraryPath := os.Getenv("LD_LIBRARY_PATH")
		newPath := strings.Join(nvidiaPaths, string(os.PathListSeparator))
		if ldLibraryPath != "" {
			newPath = newPath + string(os.PathListSeparator) + ldLibraryPath
		}

		// Update LD_LIBRARY_PATH in env
		found := false
		for i, e := range env {
			if strings.HasPrefix(e, "LD_LIBRARY_PATH=") {
				env[i] = "LD_LIBRARY_PATH=" + newPath
				found = true
				break
			}
		}
		if !found {
			env = append(env, "LD_LIBRARY_PATH="+newPath)
		}
		logger.Debug("Updated LD_LIBRARY_PATH for WhisperX", "path", newPath)
	}

	// HuggingFace token: job parameter first, then the container's HF_TOKEN.
	// WhisperX/pyannote read HF_TOKEN from the environment when --hf_token is absent.
	if hfToken := w.GetStringParameter(params, "hf_token"); hfToken != "" {
		env = setEnv(env, "HF_TOKEN", hfToken)
	}

	cmd.Env = append(env, "PYTHONUNBUFFERED=1")

	// Setup log file
	// Job log: transcript text is omitted unless SCRIBERR_LOG_TRANSCRIPTS=true
	if logW, closeLog, logErr := OpenJobLog(procCtx.OutputDirectory); logErr != nil {
		logger.Warn("Failed to create log file", "error", logErr)
	} else {
		defer closeLog()
		cmd.Stdout = logW
		cmd.Stderr = logW
	}

	logger.Info("Executing WhisperX command", "args", strings.Join(args, " "))

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.Canceled {
			return nil, fmt.Errorf("transcription was cancelled")
		}

		// Read tail of log file for context
		logPath := filepath.Join(procCtx.OutputDirectory, "transcription.log")
		logTail, readErr := w.ReadLogTail(logPath, 2048)
		if readErr != nil {
			logger.Warn("Failed to read log tail", "error", readErr)
		}

		logger.Error("WhisperX execution failed", "error", err)
		return nil, fmt.Errorf("WhisperX execution failed: %w\nLogs:\n%s", err, logTail)
	}

	// Parse result
	result, err := w.parseResult(tempDir, input, params)
	if err != nil {
		return nil, fmt.Errorf("failed to parse result: %w", err)
	}

	result.ProcessingTime = time.Since(startTime)
	result.ModelUsed = w.GetStringParameter(params, "model")
	result.Metadata = w.CreateDefaultMetadata(params)

	logger.Info("WhisperX transcription completed",
		"segments", len(result.Segments),
		"words", len(result.WordSegments),
		"processing_time", result.ProcessingTime)

	return result, nil
}

// buildWhisperXArgs builds the command arguments for WhisperX
func (w *WhisperXAdapter) buildWhisperXArgs(input interfaces.AudioInput, params map[string]interface{}, outputDir string) ([]string, error) {
	whisperxPath := filepath.Join(w.envPath, "WhisperX")

	args := []string{
		"run", UVTLSFlag, "--project", whisperxPath, "python", "-m", "whisperx",
		input.FilePath,
		"--output_dir", outputDir,
	}

	// Core parameters
	args = append(args, "--model", w.GetStringParameter(params, "model"))
	args = append(args, "--device", w.GetStringParameter(params, "device"))
	args = append(args, "--device_index", strconv.Itoa(w.GetIntParameter(params, "device_index")))
	args = append(args, "--batch_size", strconv.Itoa(w.GetIntParameter(params, "batch_size")))
	args = append(args, "--compute_type", w.GetStringParameter(params, "compute_type"))

	if threads := w.GetIntParameter(params, "threads"); threads > 0 {
		args = append(args, "--threads", strconv.Itoa(threads))
	}

	// Output settings
	args = append(args, "--output_format", "all")
	args = append(args, "--verbose", "True")

	// Task and language
	args = append(args, "--task", w.GetStringParameter(params, "task"))
	if language := w.GetStringParameter(params, "language"); language != "" {
		args = append(args, "--language", language)
	}

	// VAD settings
	args = append(args, "--vad_method", w.GetStringParameter(params, "vad_method"))
	args = append(args, "--vad_onset", fmt.Sprintf("%.3f", w.GetFloatParameter(params, "vad_onset")))
	args = append(args, "--vad_offset", fmt.Sprintf("%.3f", w.GetFloatParameter(params, "vad_offset")))

	// Custom alignment model
	if alignModel := w.GetStringParameter(params, "align_model"); alignModel != "" {
		args = append(args, "--align_model", alignModel)
	}

	// Diarization
	if w.GetBoolParameter(params, "diarize") {
		args = append(args, "--diarize")

		diarizeModel := w.GetStringParameter(params, "diarize_model")
		if diarizeModel == "pyannote" {
			diarizeModel = "pyannote/speaker-diarization-3.1"
		}
		args = append(args, "--diarize_model", diarizeModel)

		if minSpeakers := w.GetIntParameter(params, "min_speakers"); minSpeakers > 0 {
			args = append(args, "--min_speakers", strconv.Itoa(minSpeakers))
		}
		if maxSpeakers := w.GetIntParameter(params, "max_speakers"); maxSpeakers > 0 {
			args = append(args, "--max_speakers", strconv.Itoa(maxSpeakers))
		}
	}

	// Quality settings
	args = append(args, "--temperature", fmt.Sprintf("%.2f", w.GetFloatParameter(params, "temperature")))
	args = append(args, "--best_of", strconv.Itoa(w.GetIntParameter(params, "best_of")))
	args = append(args, "--beam_size", strconv.Itoa(w.GetIntParameter(params, "beam_size")))
	args = append(args, "--patience", fmt.Sprintf("%.2f", w.GetFloatParameter(params, "patience")))

	// HuggingFace token is passed via the HF_TOKEN environment variable (see
	// Transcribe), never on the command line where ps/docker top and
	// the "Executing WhisperX command" log line would expose it.

	// Disable print progress for cleaner output
	args = append(args, "--print_progress", "False")

	return args, nil
}

// parseResult parses the WhisperX output files
func (w *WhisperXAdapter) parseResult(outputDir string, input interfaces.AudioInput, params map[string]interface{}) (*interfaces.TranscriptResult, error) {
	// Find JSON result files
	files, err := filepath.Glob(filepath.Join(outputDir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to find result files: %w", err)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no result files found in %s", outputDir)
	}

	// Use the first JSON file found
	resultFile := files[0]

	data, err := os.ReadFile(resultFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read result file: %w", err)
	}

	// Parse WhisperX JSON format
	var whisperxResult struct {
		Segments []struct {
			Start   float64 `json:"start"`
			End     float64 `json:"end"`
			Text    string  `json:"text"`
			Speaker *string `json:"speaker,omitempty"`
		} `json:"segments"`
		Word []struct {
			Start   float64 `json:"start"`
			End     float64 `json:"end"`
			Word    string  `json:"word"`
			Score   float64 `json:"score"`
			Speaker *string `json:"speaker,omitempty"`
		} `json:"word_segments,omitempty"`
		Language string `json:"language"`
		Text     string `json:"text,omitempty"`
	}

	if err := json.Unmarshal(data, &whisperxResult); err != nil {
		return nil, fmt.Errorf("failed to parse JSON result: %w", err)
	}

	// Convert to standard format
	result := &interfaces.TranscriptResult{
		Language:     whisperxResult.Language,
		Segments:     make([]interfaces.TranscriptSegment, len(whisperxResult.Segments)),
		WordSegments: make([]interfaces.TranscriptWord, len(whisperxResult.Word)),
		Confidence:   0.0, // WhisperX doesn't provide overall confidence
	}

	// Convert segments
	var textParts []string
	for i, seg := range whisperxResult.Segments {
		result.Segments[i] = interfaces.TranscriptSegment{
			Start:   seg.Start,
			End:     seg.End,
			Text:    seg.Text,
			Speaker: seg.Speaker,
		}
		textParts = append(textParts, seg.Text)
	}

	// Convert words
	for i, word := range whisperxResult.Word {
		result.WordSegments[i] = interfaces.TranscriptWord{
			Start:   word.Start,
			End:     word.End,
			Word:    word.Word,
			Score:   word.Score,
			Speaker: word.Speaker,
		}
	}

	// Set full text
	if whisperxResult.Text != "" {
		result.Text = whisperxResult.Text
	} else {
		result.Text = strings.Join(textParts, " ")
	}

	return result, nil
}

// GetEstimatedProcessingTime provides WhisperX-specific time estimation
func (w *WhisperXAdapter) GetEstimatedProcessingTime(input interfaces.AudioInput) time.Duration {
	// WhisperX processing time varies by model size
	baseTime := w.BaseAdapter.GetEstimatedProcessingTime(input)

	// Adjust based on model size (if we can determine it)
	// This would need model size information from parameters
	// For now, use base estimation
	return baseTime
}

// findNvidiaLibPaths searches for nvidia library paths in the virtual environment
func (w *WhisperXAdapter) findNvidiaLibPaths() ([]string, error) {
	whisperxPath := filepath.Join(w.envPath, "WhisperX")

	// Find site-packages
	matches, err := filepath.Glob(filepath.Join(whisperxPath, ".venv", "lib", "python*", "site-packages"))
	if err != nil || len(matches) == 0 {
		return nil, fmt.Errorf("could not find site-packages: %v", err)
	}
	sitePackages := matches[0]

	// Find all nvidia/*/lib directories
	libMatches, err := filepath.Glob(filepath.Join(sitePackages, "nvidia", "*", "lib"))
	if err != nil {
		return nil, err
	}

	// Also include the base site-packages/nvidia directory if needed, sometimes libs are there
	// But usually they are in nvidia/<component>/lib

	// Filter out any matches that are not directories
	var paths []string
	for _, match := range libMatches {
		if info, err := os.Stat(match); err == nil && info.IsDir() {
			paths = append(paths, match)
		}
	}

	return paths, nil
}
