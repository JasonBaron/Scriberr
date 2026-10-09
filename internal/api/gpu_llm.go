package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"scriberr/internal/gpu"
	"scriberr/internal/llm"
	"scriberr/internal/models"
	"scriberr/internal/pii"
	"scriberr/internal/titles"
	"scriberr/pkg/clock"
	"scriberr/pkg/logger"
)

const (
	// EnvUnloadAfterSummary ("false" to disable) unloads the Ollama model as
	// soon as a summary finishes, freeing GPU memory for transcription.
	EnvUnloadAfterSummary = "SCRIBERR_UNLOAD_LLM_AFTER_SUMMARY"
	// EnvUnloadBeforeTranscription ("false" to disable) unloads every Ollama
	// model before a transcription starts.
	EnvUnloadBeforeTranscription = "SCRIBERR_UNLOAD_LLM_BEFORE_TRANSCRIPTION"

	gpuBusyMessage = "The GPU is busy with a transcription. Try again when it finishes."
)

func envEnabled(key string) bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(key)), "false")
}

// tryLocalLLMGPU takes the GPU lock for a local (Ollama) LLM request. Remote
// providers do not use the GPU and always succeed. ok is false if the GPU is
// busy; callers return gpuBusyMessage instead of waiting.
func tryLocalLLMGPU(svc llm.Service) (release func(), ok bool) {
	if _, local := svc.(*llm.OllamaService); !local {
		return func() {}, true
	}
	return gpu.TryAcquire()
}

// unloadAfterSummary frees the summary model's GPU memory when enabled.
func unloadAfterSummary(svc llm.Service, model string) {
	o, local := svc.(*llm.OllamaService)
	if !local || !envEnabled(EnvUnloadAfterSummary) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := o.Unload(ctx, model); err != nil {
		logger.Warn("Could not unload Ollama model after summary", "model", model, "error", err)
		return
	}
	logger.Info("Unloaded Ollama model after summary", "model", model)
}

// UnloadLocalLLM is registered with gpu.OnBeforeTranscription. It unloads
// every model the configured Ollama server holds and returns a note for the
// job log.
func (h *Handler) UnloadLocalLLM(ctx context.Context) string {
	if !envEnabled(EnvUnloadBeforeTranscription) {
		return ""
	}
	svc, _, err := h.getLLMService(ctx)
	if err != nil {
		return ""
	}
	o, local := svc.(*llm.OllamaService)
	if !local {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	names, err := o.UnloadAll(ctx)
	if err != nil {
		return "could not unload Ollama models: " + err.Error()
	}
	if len(names) == 0 {
		return ""
	}
	return "unloaded from Ollama to free GPU memory: " + strings.Join(names, ", ")
}

// afterSummary runs in the background once a summary stream has closed,
// while the model is still loaded and the GPU lock is held. It asks the same
// model for a working title, a one-sentence brief and tags (see
// suggestFromSummary).
func (h *Handler) afterSummary(ctx context.Context, req SummarizeRequest, svc llm.Service, summary string, completed bool) {
	if !envEnabled(titles.EnvSuggest) {
		return
	}
	if !completed || strings.TrimSpace(summary) == "" {
		logger.Info("Title suggestion skipped: summary did not complete", "job_id", req.TranscriptionID)
		return
	}
	if err := h.suggestFromSummary(ctx, req.TranscriptionID, req.Model, svc, summary, false); err != nil {
		logger.Warn("Title suggestion failed", "job_id", req.TranscriptionID, "model", req.Model, "error", err)
	}
}

// suggestFromSummary asks the model for a topic, brief and tags based on a
// summary and saves them. With a strict vocabulary (the default) the model
// picks one recording type and up to three topics from the fixed lists and
// code adds youtube, sensitive and pii. Otherwise tags are free-form,
// steered toward ones already in use. The title is applied when the current
// one is a placeholder; tags are applied unless the user edited them, or
// always when overwriteEdited is set.
func (h *Handler) suggestFromSummary(ctx context.Context, jobID, model string, svc llm.Service, summary string, overwriteEdited bool) error {
	job, err := h.jobRepo.FindByID(ctx, jobID)
	if err != nil || job == nil {
		return errors.New("recording not found")
	}
	settings := h.summarySettings(ctx)
	current := ""
	if job.Title != nil {
		current = *job.Title
	}
	name := current
	if name == "" {
		name = filepath.Base(job.AudioPath)
	}
	flags := titles.Flags{YouTube: isYouTube(job)}
	if job.Transcript != nil {
		if kinds := pii.Detect(transcriptText(*job.Transcript)); len(kinds) > 0 {
			flags.PII = true
			logger.Info("Personal identifiers found in transcript", "job_id", job.ID, "kinds", strings.Join(kinds, ","))
		}
	}

	strict := settings.TagStrict == nil || *settings.TagStrict
	voc := titles.NewVocabulary(settings.TagTypes, settings.TagTopics, settings.TagSynonyms).WithNameHints(settings.TagNameHints)
	if own := ownName(job); own != "" && strict {
		flags.NameType = voc.TypeFromName(own)
	}
	keywords := keywordCount(settings)
	var vocabulary, known []string
	prompt := ""
	if strict {
		known = h.knownKeywords(ctx, voc)
		opening := ""
		if job.Transcript != nil {
			opening = transcriptOpening(*job.Transcript, 2500)
		}
		prompt = voc.StrictPrompt(summary, name, opening, keywords, known)
	} else {
		vocabulary = h.tagVocabulary(ctx)
		prompt = titles.Prompt(summary, vocabulary)
	}

	// Short, deterministic call: temperature 0 so the same summary gets the
	// same tags. Thinking is off regardless of the template: a title does
	// not benefit from it and it would only add latency.
	tctx, cancel := context.WithTimeout(llm.WithDeterministic(llm.WithThinking(context.Background(), false)), 90*time.Second)
	defer cancel()
	messages := []llm.ChatMessage{{Role: "user", Content: prompt}}
	ask := func() (string, error) {
		resp, err := svc.ChatCompletion(tctx, model, messages, 0)
		if err != nil || resp == nil || len(resp.Choices) == 0 {
			if err == nil {
				err = errors.New("empty reply")
			}
			return "", err
		}
		return resp.Choices[0].Message.Content, nil
	}
	raw, err := ask()
	if err != nil {
		return err
	}
	var topic, brief string
	var tags, flagTags []string
	if strict {
		c, perr := voc.ParseStrict(raw)
		// A reply with no usable type or topics gets one correction.
		if perr != nil || c.Type == "" || len(c.Topics) == 0 {
			messages = append(messages,
				llm.ChatMessage{Role: "assistant", Content: raw},
				llm.ChatMessage{Role: "user", Content: "That reply is missing a valid type or topics. Reply again with the same JSON shape only. type must be exactly one recording type from the list, and topics must hold 1 to 3 topics from the list, spelled exactly as shown."})
			if raw2, err := ask(); err == nil {
				if c2, err2 := voc.ParseStrict(raw2); err2 == nil && (perr != nil || (c2.Type != "" && len(c2.Topics) > 0)) {
					c, perr = c2, nil
				}
			}
		}
		if perr != nil {
			return fmt.Errorf("unusable reply: %w", perr)
		}
		topic, brief = c.Topic, c.Brief
		tags, flagTags = voc.Assemble(c, flags, keywords, known)
	} else {
		sug, err := titles.Parse(raw, vocabulary)
		if err != nil {
			return fmt.Errorf("unusable reply: %w", err)
		}
		topic, brief = sug.Topic, sug.Brief
		tags, flagTags = titles.SplitFlags(sug.Tags, flags)
	}
	if settings.RedactPII == nil || *settings.RedactPII {
		brief = pii.Redact(brief)
	}

	h.ensureRecordedAt(ctx, job)
	title := titles.Format(titles.Pattern(), jobDate(job).In(clock.Display), topic)
	apply := titles.IsPlaceholder(current, job.AudioPath)
	sug := models.JobSuggestion{Title: title, Brief: brief, Tags: models.StringList(tags), Flags: models.StringList(flagTags), OverwriteEditedTags: overwriteEdited}
	if err := h.summaryRepo.SaveSuggestions(ctx, job.ID, sug, apply); err != nil {
		return err
	}
	logger.Info("Saved title suggestion", "job_id", job.ID, "title", title, "tags", strings.Join(tags, ","), "flags", strings.Join(flagTags, ","), "applied", apply)
	return nil
}

// keywordCount is how many free-form keywords to ask for (0 to 5).
func keywordCount(s *models.SummarySetting) int {
	if s.TagKeywords == nil {
		return titles.DefaultKeywords
	}
	n := *s.TagKeywords
	if n < 0 {
		return 0
	}
	if n > titles.MaxKeywords {
		return titles.MaxKeywords
	}
	return n
}

// knownKeywords returns the free-form keywords in use, most used first:
// every tag that is not a recording type, a topic or youtube.
func (h *Handler) knownKeywords(ctx context.Context, voc titles.Vocabulary) []string {
	var out []string
	for _, t := range h.tagVocabulary(ctx) {
		if t == titles.TagYouTube || titles.IsFlag(t) || voc.ResolveType(t) == t || voc.ResolveTopic(t) == t {
			continue
		}
		out = append(out, t)
	}
	return out
}

// MigrateFlags moves sensitive and pii out of tags into flags (once; later
// runs find nothing to move).
func (h *Handler) MigrateFlags(ctx context.Context) {
	n, err := h.summaryRepo.MoveTagsToFlags(ctx, []string{titles.TagSensitive, titles.TagPII})
	if err != nil {
		logger.Warn("Could not move sensitive and pii tags to flags", "error", err)
		return
	}
	if n > 0 {
		logger.Info("Moved sensitive and pii from tags to flags", "recordings", n)
	}
}

// ownName is the name the person gave the recording: the uploaded file's
// name, else a title they set themselves (not one Scriberr suggested).
func ownName(job *models.TranscriptionJob) string {
	if strings.TrimSpace(job.OriginalFilename) != "" {
		return job.OriginalFilename
	}
	if job.Title == nil || strings.TrimSpace(*job.Title) == "" {
		return ""
	}
	if job.SuggestedTitle != nil && *job.SuggestedTitle == *job.Title {
		return ""
	}
	if isYouTube(job) {
		return ""
	}
	return *job.Title
}

// summarySettings returns the saved settings, or defaults.
func (h *Handler) summarySettings(ctx context.Context) *models.SummarySetting {
	if s, err := h.summaryRepo.GetSettings(ctx); err == nil && s != nil {
		return s
	}
	return &models.SummarySetting{}
}

var youTubeTitleRe = regexp.MustCompile(`(?i)\byoutube:`)

// isYouTube reports whether a recording came from YouTube: downloaded
// through Scriberr, or titled "... YouTube: ..." by hand.
func isYouTube(job *models.TranscriptionJob) bool {
	if job.SourceURL != nil && isYouTubeURL(*job.SourceURL) {
		return true
	}
	return job.Title != nil && youTubeTitleRe.MatchString(*job.Title)
}

func isYouTubeURL(u string) bool {
	return strings.Contains(u, "youtube.com") || strings.Contains(u, "youtu.be")
}

// transcriptOpening returns about the first max characters of a stored
// transcript, with speaker labels, cut at a line.
func transcriptOpening(transcriptJSON string, max int) string {
	var t storedTranscript
	if err := json.Unmarshal([]byte(transcriptJSON), &t); err != nil {
		return ""
	}
	var b strings.Builder
	for _, seg := range t.Segments {
		line := strings.TrimSpace(seg.Text)
		if seg.Speaker != "" {
			line = "[" + seg.Speaker + "] " + line
		}
		if b.Len()+len(line) > max {
			break
		}
		b.WriteString(line + "\n")
	}
	if b.Len() == 0 {
		text := strings.TrimSpace(t.Text)
		if len(text) > max {
			if cut := strings.LastIndex(text[:max], " "); cut > 0 {
				text = text[:cut]
			} else {
				text = text[:max]
			}
		}
		return text
	}
	return strings.TrimSpace(b.String())
}

// transcriptText returns the plain text of a stored transcript.
func transcriptText(transcriptJSON string) string {
	var t storedTranscript
	if err := json.Unmarshal([]byte(transcriptJSON), &t); err != nil {
		return ""
	}
	if strings.TrimSpace(t.Text) != "" {
		return t.Text
	}
	parts := make([]string, 0, len(t.Segments))
	for _, s := range t.Segments {
		parts = append(parts, s.Text)
	}
	return strings.Join(parts, " ")
}

// tagVocabulary returns the tags in use, most used first.
func (h *Handler) tagVocabulary(ctx context.Context) []string {
	counts, err := h.summaryRepo.TagCounts(ctx)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(counts))
	for _, c := range counts {
		out = append(out, c.Tag)
	}
	return out
}
