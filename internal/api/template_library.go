package api

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"scriberr/internal/models"
	"scriberr/pkg/logger"
)

// libraryTemplate is a summary template shipped with the fork. They are
// added on startup (see SeedTemplates) and can be edited, disabled and
// reset. Auto tags use the recording types and topics of the default tag
// vocabulary, so each one runs on the right recordings automatically.
type libraryTemplate struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Prompt      string   `json:"prompt"`
	AutoTags    []string `json:"auto_tags"`
	Speakers    bool     `json:"include_speaker_info"`
	// Replaces lists older names this template updates in place, keeping
	// the existing summaries linked to it.
	Replaces []string `json:"replaces,omitempty"`
}

// Shared rules at the top of every library prompt. {me} is filled from
// Settings > Summary > Your name.
const libraryRules = `Rules:
- Use only what is in the transcript. Do not invent names, numbers, dates, decisions or quotes.
- If something is unclear or inaudible, say so briefly instead of guessing.
- Do not diagnose anyone or give clinical, legal or financial advice.
- Mark anything that is your interpretation rather than something said with "(inferred)".
- Refer to people by the names in the speaker labels. {me} is the person this is written for; address {me} as "you".
- Leave out any section that has nothing real to say. No introduction, no closing remarks, no restating these instructions.
- Plain Markdown with the headings below.
`

var templateLibrary = []libraryTemplate{
	{
		Key:         "default",
		Name:        "Default",
		Description: "Short general summary of any recording. Also used to suggest the title, brief and tags.",
		Speakers:    true,
		Prompt: `Summarize this recording for someone who was not there and will reread it later.

` + libraryRules + `
## Overview
2 to 3 sentences: what this was, who took part, and the main point.

## Key Points
3 to 7 bullets, most important first. One idea per bullet, specific rather than general.

## Decisions
Bullets of anything agreed or settled, with who agreed if clear.

## Action Items
Bullets in the form: owner: task (due date if stated). Only tasks someone actually took on.

## Open Questions
Bullets of anything raised but not resolved.

## Notable Quotes
Up to 3 short direct quotes that capture something important, with the speaker.

Keep the whole summary under 350 words.`,
	},
	{
		Key:         "individual-therapy",
		Name:        "Individual Therapy Session",
		Description: "Reflection notes from an individual therapy session.",
		AutoTags:    []string{"individual therapy"},
		Speakers:    true,
		Replaces:    []string{"Therapy Discussion - Single"},
		Prompt: `Write reflection notes from this individual therapy session for {me}, the client. You are not the therapist.

` + libraryRules + `
## Session Overview
2 to 4 sentences on the main themes, factual and neutral.

## What You Brought In
Bullets of the situations, worries or questions you raised.

## Themes and Patterns
Bullets of feelings, thoughts, reactions or relationship patterns that came up. Separate what you said directly from what is inferred.

## What the Therapist Focused On
Bullets of the therapist's questions, reflections, reframes or suggestions, in plain words.

## Moments That Mattered
Up to 4 short quotes or moments, each with one line on why it may matter.

## Growth Signals
Bullets of insight, accountability, honesty or willingness to change.

## Homework and Practice
Bullets of anything agreed to try before the next session.

## Questions for Next Session
4 to 6 open questions that explore patterns rather than force conclusions.

Tone: direct, grounded and honest. Do not flatter, catastrophize or turn this into a motivational speech. Under 600 words.`,
	},
	{
		Key:         "couples-therapy",
		Name:        "Couples Therapy Session",
		Description: "Patterns, rupture, repair and shared work from a couples therapy session.",
		AutoTags:    []string{"couples therapy"},
		Speakers:    true,
		Replaces:    []string{"Couples Therapy Review"},
		Prompt: `Summarize this couples therapy session for {me}, one of the partners.

` + libraryRules + `- Do not take sides or declare the relationship healthy or unhealthy.

## Session Overview
2 to 4 sentences on the main topics, neutral and factual.

## Each Partner's Experience
For each partner, bullets of what they said they feel, need, fear or ask for. Use their own words where possible.

## Interaction Pattern
The repeated cycle between the partners (for example pursue and withdraw, explain and defend), only if the transcript shows it.

## Rupture and Repair
Bullets of moments where tension rose and moments where someone softened, clarified, listened or took responsibility.

## Therapist's Focus
Bullets of the therapist's questions, observations, reframes and any homework.

## Your Part
Bullets of what you could reflect on, own or practice. Accountable, not self-blaming.

## Shared Work
Bullets of practical, low-pressure things to try together.

## Questions for Next Session
4 to 6 questions to bring back.

Tone: neutral, careful and constructive. Under 650 words.`,
	},
	{
		Key:         "relationship-talk",
		Name:        "Relationship Talk Review",
		Description: "Needs, escalation, repair and follow-up from a conversation between partners.",
		AutoTags:    []string{"relationship talk"},
		Speakers:    true,
		Replaces:    []string{"Relationship Discussion Review"},
		Prompt: `Review this conversation between partners for {me}.

` + libraryRules + `- Do not take sides or assume intent.

## What It Was About
2 to 3 sentences, neutral and factual.

## What Each Person Asked For
For each person, bullets of stated needs, concerns, boundaries or requests. Separate what was said from what is inferred.

## Turning Points
Bullets of moments the conversation got tense or confused, and what seemed to trigger the shift.

## Repair Attempts
Bullets of moments either person softened, clarified, apologized or reassured.

## Different Meanings
Words or assumptions that may have meant different things to each person, without blame.

## Your Part
Bullets of where you may have pushed, defended, over-explained, withdrawn or missed a signal. Honest, not self-attacking.

## Agreements and Open Items
Bullets of anything agreed, and anything left unresolved.

## Follow-up
2 to 4 short, low-pressure talking points or a brief message in your voice: direct, grounded, accountable.

Under 600 words.`,
	},
	{
		Key:         "argument-review",
		Name:        "Argument Review",
		Description: "Triggers, escalation, ownership and repair after a heated disagreement.",
		AutoTags:    []string{"conflict"},
		Speakers:    true,
		Replaces:    []string{"Argument Review"},
		Prompt: `Review this argument or heated disagreement for {me}.

` + libraryRules + `- Do not take sides, assume intent or excuse harmful behavior.

## What Happened
2 to 3 neutral sentences.

## Core Issue
The likely issue beneath the words. Separate the explicit topic from inferred emotional drivers.

## Escalation Map
For each moment the conflict intensified: what was said, how it may have landed, and how the tone changed.

## Your Contribution
Bullets of where you may have escalated, interrupted, defended, minimized or pushed for resolution.

## Their Possible Experience
What the other person may have felt or needed, labeled (inferred).

## Missed Repair Opportunities
Bullets of places either person could have paused, validated, clarified or apologized.

## Better Version
Rewrite 2 to 4 key moments as healthier alternatives in your voice: direct, grounded, accountable, not overly soft.

## Follow-up Message
A short message that owns your part without over-apologizing or pressuring for an immediate answer.

## Lessons
3 to 5 practical reminders for next time.

Under 650 words.`,
	},
	{
		Key:         "separation-planning",
		Name:        "Separation Planning",
		Description: "Practical decisions and open items when separating: finances, housing, children, agreements.",
		AutoTags:    []string{"separation"},
		Speakers:    true,
		Prompt: `Pull the practical side out of this conversation about separating, for {me}.

` + libraryRules + `- Keep emotional content brief; focus on logistics and agreements.

## Overview
2 to 3 neutral sentences.

## Agreed
Bullets of anything both people agreed to, with dates or amounts if stated.

## Proposed but Not Agreed
Bullets of suggestions or positions, with who made them.

## By Area
Short bullets under each heading that applies: Finances, Housing and Property, Children and Schedule, Pets, Belongings, Communication Ground Rules.

## Deadlines and Next Steps
Bullets in the form: owner: task (date if stated).

## Questions for a Lawyer, Mediator or Accountant
Bullets of points that need professional input.

Under 500 words.`,
	},
	{
		Key:         "medical-appointment",
		Name:        "Medical Appointment Notes",
		Description: "Symptoms, findings, tests, treatment plan and follow-ups from a medical visit.",
		AutoTags:    []string{"medical appointment"},
		Speakers:    true,
		Prompt: `Write notes from this medical appointment for {me}, the patient, to reread before the next visit.

` + libraryRules + `- Record what the clinician said, not your own medical opinion. Keep medication names, doses and numbers exactly as spoken.

## Visit Overview
Who you saw (role or specialty if stated), why, and the main conclusion, in 2 to 3 sentences.

## Symptoms and History Discussed
Bullets.

## Findings and Results
Bullets of exam findings and test results that were explained.

## Assessment
What the clinician thinks is going on, in their words. Say clearly if nothing was concluded.

## Treatment Plan
Bullets of medications (name, dose, timing), therapy, exercises or procedures.

## Tests and Referrals
Bullets of tests ordered, referrals and specialists, with timing.

## Instructions and Warning Signs
Bullets of what to do, avoid, or watch for, and when to call.

## Follow-up
Next appointment or check-in, if set.

## Questions for Next Visit
3 to 6 questions that came up or were left unanswered.

Under 550 words.`,
	},
	{
		Key:         "advisor-meeting",
		Name:        "Advisor Meeting Notes",
		Description: "Advice, decisions, documents, costs and deadlines from a lawyer, mediator, accountant or other advisor.",
		AutoTags:    []string{"advisor meeting"},
		Speakers:    true,
		Prompt: `Write notes from this meeting with a professional advisor (for example a lawyer, mediator, accountant, financial advisor or realtor) for {me}.

` + libraryRules + `- Record the advisor's advice as stated; do not add your own.

## Overview
Who the advisor was (role), the purpose, and the outcome, in 2 to 3 sentences.

## Advice Given
Bullets, each as specific as the transcript allows.

## Options Discussed
Bullets of options with the pros, cons, costs or risks mentioned.

## Decisions
Bullets of what was decided.

## Documents and Information Needed
Bullets of what to gather or send, and to whom.

## Costs and Fees
Bullets of any amounts, rates or estimates mentioned.

## Deadlines and Next Steps
Bullets in the form: owner: task (date if stated).

## Open Questions
Bullets of what still needs an answer.

Under 500 words.`,
	},
	{
		Key:         "work-meeting",
		Name:        "Work Meeting Notes",
		Description: "Decisions, action items, risks and open questions from a work meeting.",
		AutoTags:    []string{"work meeting"},
		Speakers:    true,
		Prompt: `Write meeting notes for {me} to share with the team or file.

` + libraryRules + `
## Summary
2 to 3 sentences: purpose, participants and outcome.

## Decisions
Bullets of what was decided and by whom.

## Action Items
Bullets in the form: owner: task (due date if stated). Only tasks someone took on.

## Key Discussion Points
3 to 7 bullets, most important first, with numbers and dates as stated.

## Risks and Blockers
Bullets of risks, dependencies or concerns raised.

## Open Questions
Bullets of what is unresolved and who should answer.

Keep it factual and skimmable. Under 450 words.`,
	},
	{
		Key:         "media-notes",
		Name:        "Media Notes",
		Description: "Key ideas, takeaways and quotes from a video, podcast, lecture or guided practice.",
		AutoTags:    []string{"media"},
		Speakers:    false,
		Prompt: `Write study notes on this recorded content (a video, podcast, lecture, audiobook or guided practice) for {me}.

` + libraryRules + `- Present the speaker's claims as their views, not as established fact.

## What It Is
1 to 2 sentences: the speaker or source if stated, the format, and the main thesis.

## Key Ideas
4 to 8 bullets, each one idea explained in a sentence.

## Practical Takeaways
3 to 5 bullets of things you could apply or try.

## Notable Quotes
Up to 3 short direct quotes.

## Worth Questioning
1 to 3 bullets of claims that seem weak, one-sided or need a source.

Under 450 words.`,
	},
	{
		Key:         "personal-note",
		Name:        "Personal Note",
		Description: "Themes, decisions and reminders from a voice memo or journal entry.",
		AutoTags:    []string{"personal note"},
		Speakers:    false,
		Prompt: `Turn this voice memo or journal entry by {me} into a clean note to reread later.

` + libraryRules + `
## Summary
2 to 3 sentences in plain words.

## Thoughts and Feelings
Bullets of what was on your mind. Keep your meaning; tidy the wording.

## Decisions and Intentions
Bullets of anything you decided or want to do.

## Reminders and To-dos
Bullets in the form: task (date if stated).

## Ideas to Revisit
Bullets of loose threads worth coming back to.

Under 350 words.`,
	},
	{
		Key:         "general-summary",
		Name:        "General Transcript Summary",
		Description: "Detailed, balanced summary of any transcript, with important details and speaker perspectives.",
		Speakers:    true,
		Replaces:    []string{"General Transcript Summary"},
		Prompt: `Write a detailed, balanced summary of this transcript for {me}.

` + libraryRules + `
## Overview
The purpose and topic in 2 to 3 sentences.

## Main Topics
Bullets.

## Key Points
Concise bullets of the most important points, decisions, concerns or observations.

## Important Details
Names, dates, times, numbers, commitments and references that may matter later.

## Speaker Perspectives
For each speaker, what they contributed or emphasized. Separate direct statements from inferred meaning.

## Action Items
Owners, tasks and deadlines. If none, write "No clear action items."

## Open Questions
Bullets.

## Notable Quotes
Up to 3 short quotes, only when they add value.

## For Future Review
A 2 sentence recap to jog your memory later.

Under 700 words.`,
	},
}

func builtinByKey(key string) *libraryTemplate {
	for i := range templateLibrary {
		if templateLibrary[i].Key == key {
			return &templateLibrary[i]
		}
	}
	return nil
}

// findLibraryMatch finds an unlinked template with the library template's
// name or one of its older names.
func findLibraryMatch(lib libraryTemplate, items []models.SummaryTemplate) *models.SummaryTemplate {
	for _, name := range append([]string{lib.Name}, lib.Replaces...) {
		for i := range items {
			if items[i].BuiltinKey == "" && strings.EqualFold(strings.TrimSpace(items[i].Name), name) {
				return &items[i]
			}
		}
	}
	return nil
}

func sameAsLibrary(lib libraryTemplate, t *models.SummaryTemplate) bool {
	desc := ""
	if t.Description != nil {
		desc = *t.Description
	}
	if t.Name != lib.Name || strings.TrimSpace(t.Prompt) != strings.TrimSpace(lib.Prompt) || desc != lib.Description || t.IncludeSpeakerInfo != lib.Speakers {
		return false
	}
	if len(t.AutoTags) != len(lib.AutoTags) {
		return false
	}
	for i := range lib.AutoTags {
		if t.AutoTags[i] != lib.AutoTags[i] {
			return false
		}
	}
	return true
}

// applyLibrary copies the shipped content onto a template. Model,
// Reasoning, Enabled and Default are left alone.
func applyLibrary(t *models.SummaryTemplate, lib libraryTemplate) {
	desc := lib.Description
	t.Name, t.Description, t.Prompt = lib.Name, &desc, lib.Prompt
	t.AutoTags = models.StringList(append([]string{}, lib.AutoTags...))
	t.IncludeSpeakerInfo = lib.Speakers
	t.BuiltinKey = lib.Key
}

// annotateTemplates marks built-in templates that differ from the shipped
// version.
func annotateTemplates(items []models.SummaryTemplate) {
	for i := range items {
		if lib := builtinByKey(items[i].BuiltinKey); lib != nil {
			items[i].Customized = !sameAsLibrary(*lib, &items[i])
		}
	}
}

// libraryModel picks a model for templates that have none: the model set in
// Summary settings, else the default template's, else the model most
// templates use. hasDefault reports whether a default template exists.
func libraryModel(items []models.SummaryTemplate, settingsModel string) (model string, hasDefault bool) {
	counts := map[string]int{}
	for _, t := range items {
		if t.IsDefault {
			hasDefault = true
			if strings.TrimSpace(t.Model) != "" {
				model = t.Model
			}
		}
		if m := strings.TrimSpace(t.Model); m != "" {
			counts[m]++
		}
	}
	if m := strings.TrimSpace(settingsModel); m != "" {
		return m, hasDefault
	}
	if model != "" {
		return model, hasDefault
	}
	best := 0
	for m, n := range counts {
		if n > best || (n == best && m < model) {
			model, best = m, n
		}
	}
	return model, hasDefault
}

// pickModel returns a model for templates without one, asking the LLM
// provider for its models when nothing is configured yet. "" when none is
// available (no provider set up).
func (h *Handler) pickModel(ctx context.Context, items []models.SummaryTemplate) string {
	if m, _ := libraryModel(items, h.summarySettings(ctx).DefaultModel); m != "" {
		return m
	}
	svc, _, err := h.getLLMService(ctx)
	if err != nil {
		return ""
	}
	mctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	names, err := svc.GetModels(mctx)
	if err != nil || len(names) == 0 {
		return ""
	}
	return names[0]
}

// SeedTemplates adds the shipped templates on startup. A template made
// earlier with the same (or an older) name is linked rather than
// duplicated, and its content is kept. Templates without a model get one
// when a model is available. When no template is the default, the shipped
// Default becomes it.
func (h *Handler) SeedTemplates(ctx context.Context) {
	items, _, err := h.summaryRepo.List(ctx, 0, 1000)
	if err != nil {
		logger.Warn("Could not load summary templates", "error", err)
		return
	}
	have := map[string]bool{}
	for _, t := range items {
		if t.BuiltinKey != "" {
			have[t.BuiltinKey] = true
		}
	}
	added, linked := 0, 0
	for _, lib := range templateLibrary {
		if have[lib.Key] {
			continue
		}
		if t := findLibraryMatch(lib, items); t != nil {
			t.BuiltinKey = lib.Key
			if err := h.summaryRepo.Update(ctx, t); err == nil {
				linked++
			}
			continue
		}
		t := models.SummaryTemplate{}
		applyLibrary(&t, lib)
		if err := h.summaryRepo.Create(ctx, &t); err != nil {
			logger.Warn("Could not add built-in template", "name", lib.Name, "error", err)
			continue
		}
		items = append(items, t)
		added++
	}
	h.fillTemplateModels(ctx, items)
	h.ensureDefaultTemplate(ctx)
	if added+linked > 0 {
		logger.Info("Built-in summary templates ready", "added", added, "linked", linked)
	}
}

// fillTemplateModels gives templates without a model the one pickModel
// chooses. Returns true when any template changed.
func (h *Handler) fillTemplateModels(ctx context.Context, items []models.SummaryTemplate) bool {
	var empty []*models.SummaryTemplate
	for i := range items {
		if strings.TrimSpace(items[i].Model) == "" {
			empty = append(empty, &items[i])
		}
	}
	if len(empty) == 0 {
		return false
	}
	model := h.pickModel(ctx, items)
	if model == "" {
		return false
	}
	for _, t := range empty {
		t.Model = model
		if err := h.summaryRepo.Update(ctx, t); err != nil {
			logger.Warn("Could not set template model", "name", t.Name, "error", err)
		}
	}
	logger.Info("Set model on summary templates", "model", model, "count", len(empty))
	return true
}

// ensureDefaultTemplate makes the shipped Default the default template when
// none is marked.
func (h *Handler) ensureDefaultTemplate(ctx context.Context) {
	items, _, err := h.summaryRepo.List(ctx, 0, 1000)
	if err != nil {
		return
	}
	for _, t := range items {
		if t.IsDefault {
			return
		}
	}
	for _, t := range items {
		if t.BuiltinKey == "default" {
			if err := h.summaryRepo.SetDefaultTemplate(ctx, t.ID); err == nil {
				logger.Info("Marked the built-in Default template as default")
			}
			return
		}
	}
}

// ResetSummaryTemplate restores a built-in template
// @Summary Reset a built-in template
// @Description Restores the shipped name, description, prompt, auto tags and speaker setting. Model, Reasoning, Enabled and Default are kept.
// @Tags summaries
// @Produce json
// @Param id path string true "Template ID"
// @Success 200 {object} models.SummaryTemplate
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security ApiKeyAuth
// @Security BearerAuth
// @Router /api/v1/summaries/{id}/reset [post]
func (h *Handler) ResetSummaryTemplate(c *gin.Context) {
	ctx := c.Request.Context()
	t, err := h.summaryRepo.FindByID(ctx, c.Param("id"))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Template not found"})
		return
	}
	lib := builtinByKey(t.BuiltinKey)
	if lib == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Only built-in templates can be reset"})
		return
	}
	applyLibrary(t, *lib)
	if err := h.summaryRepo.Update(ctx, t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset template"})
		return
	}
	out := []models.SummaryTemplate{*t}
	annotateTemplates(out)
	c.JSON(http.StatusOK, out[0])
}

// PatchTemplateRequest changes the fields that are set: the model and
// whether the template is on.
type PatchTemplateRequest struct {
	Model   *string `json:"model"`
	Enabled *bool   `json:"enabled"`
}

// PatchSummaryTemplate changes a template's model or on/off state
// @Summary Change a template's model or on/off state
// @Description Disabled templates are kept but never run automatically and are hidden from the Summarize dialog. The default template cannot be disabled.
// @Tags summaries
// @Accept json
// @Produce json
// @Param id path string true "Template ID"
// @Param request body PatchTemplateRequest true "Fields to change"
// @Success 200 {object} models.SummaryTemplate
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security ApiKeyAuth
// @Security BearerAuth
// @Router /api/v1/summaries/{id} [patch]
func (h *Handler) PatchSummaryTemplate(c *gin.Context) {
	var req PatchTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Model == nil && req.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Nothing to change: send model or enabled"})
		return
	}
	ctx := c.Request.Context()
	t, err := h.summaryRepo.FindByID(ctx, c.Param("id"))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Template not found"})
		return
	}
	if req.Model != nil {
		m := strings.TrimSpace(*req.Model)
		if m == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Model cannot be empty"})
			return
		}
		t.Model = m
	}
	if req.Enabled != nil {
		if t.IsDefault && !*req.Enabled {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The default template cannot be disabled. Make another template the default first."})
			return
		}
		v := *req.Enabled
		t.Enabled = &v
	}
	if err := h.summaryRepo.Update(ctx, t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update template"})
		return
	}
	out := []models.SummaryTemplate{*t}
	annotateTemplates(out)
	c.JSON(http.StatusOK, out[0])
}

// SetSummaryTemplateEnabled enables or disables a template
// @Summary Enable or disable a template
// @Description Same as PATCH /api/v1/summaries/{id} with only "enabled".
// @Tags summaries
// @Accept json
// @Produce json
// @Param id path string true "Template ID"
// @Param request body PatchTemplateRequest true "Enabled"
// @Success 200 {object} models.SummaryTemplate
// @Security ApiKeyAuth
// @Security BearerAuth
// @Router /api/v1/summaries/{id}/enabled [put]
func (h *Handler) SetSummaryTemplateEnabled(c *gin.Context) { h.PatchSummaryTemplate(c) }

// sortTemplates orders templates: the default, then built-ins in shipped
// order, then the rest by name.
func sortTemplates(items []models.SummaryTemplate) {
	rank := func(t models.SummaryTemplate) int {
		if t.IsDefault {
			return -1
		}
		for i, lib := range templateLibrary {
			if lib.Key == t.BuiltinKey {
				return i
			}
		}
		return len(templateLibrary)
	}
	sort.SliceStable(items, func(i, j int) bool {
		ri, rj := rank(items[i]), rank(items[j])
		if ri != rj {
			return ri < rj
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
}
