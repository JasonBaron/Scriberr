package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"scriberr/internal/models"
)

// libraryTemplate is a recommended summary template shipped with the fork.
// Auto tags use the recording types and topics of the default tag
// vocabulary, so each one runs on the right recordings automatically.
type libraryTemplate struct {
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

// LibraryItem is a recommended template and how it compares to what is
// installed.
type LibraryItem struct {
	libraryTemplate
	// Status is "missing", "installed" (same content) or "different".
	Status       string `json:"status"`
	ExistingID   string `json:"existing_id,omitempty"`
	ExistingName string `json:"existing_name,omitempty"`
}

func findLibraryMatch(lib libraryTemplate, items []models.SummaryTemplate) *models.SummaryTemplate {
	for _, name := range append([]string{lib.Name}, lib.Replaces...) {
		for i := range items {
			if strings.EqualFold(strings.TrimSpace(items[i].Name), name) {
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

// ListTemplateLibrary lists the recommended templates
// @Summary Recommended summary templates
// @Description Templates shipped with the fork and whether each is installed, missing or different from the installed version
// @Tags summaries
// @Produce json
// @Success 200 {array} LibraryItem
// @Security ApiKeyAuth
// @Security BearerAuth
// @Router /api/v1/summaries/library [get]
func (h *Handler) ListTemplateLibrary(c *gin.Context) {
	items, _, err := h.summaryRepo.List(c.Request.Context(), 0, 1000)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list templates"})
		return
	}
	out := make([]LibraryItem, 0, len(templateLibrary))
	for _, lib := range templateLibrary {
		it := LibraryItem{libraryTemplate: lib, Status: "missing"}
		if t := findLibraryMatch(lib, items); t != nil {
			it.ExistingID, it.ExistingName, it.Status = t.ID, t.Name, "different"
			if sameAsLibrary(lib, t) {
				it.Status = "installed"
			}
		}
		out = append(out, it)
	}
	c.JSON(http.StatusOK, out)
}

// ApplyTemplateLibraryRequest names the library templates to install.
type ApplyTemplateLibraryRequest struct {
	Names []string `json:"names" binding:"required,min=1"`
}

// ApplyTemplateLibrary installs or updates recommended templates
// @Summary Install recommended templates
// @Description Creates missing templates and updates installed ones (matched by name or an older name) in place: name, description, prompt, auto tags and speaker setting. Model, Reasoning and Default are kept. New templates use the default template's model.
// @Tags summaries
// @Accept json
// @Produce json
// @Param request body ApplyTemplateLibraryRequest true "Template names"
// @Success 200 {object} map[string]int
// @Failure 400 {object} map[string]string
// @Security ApiKeyAuth
// @Security BearerAuth
// @Router /api/v1/summaries/library [post]
func (h *Handler) ApplyTemplateLibrary(c *gin.Context) {
	var req ApplyTemplateLibraryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	items, _, err := h.summaryRepo.List(ctx, 0, 1000)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list templates"})
		return
	}
	model, hasDefault := "", false
	for _, t := range items {
		if t.IsDefault {
			model, hasDefault = t.Model, true
		}
	}
	if model == "" {
		model = h.summarySettings(ctx).DefaultModel
	}
	want := map[string]bool{}
	for _, n := range req.Names {
		want[strings.ToLower(strings.TrimSpace(n))] = true
	}
	created, updated := 0, 0
	for _, lib := range templateLibrary {
		if !want[strings.ToLower(lib.Name)] {
			continue
		}
		desc := lib.Description
		if t := findLibraryMatch(lib, items); t != nil {
			t.Name, t.Description, t.Prompt = lib.Name, &desc, lib.Prompt
			t.AutoTags = models.StringList(append([]string{}, lib.AutoTags...))
			t.IncludeSpeakerInfo = lib.Speakers
			if err := h.summaryRepo.Update(ctx, t); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update " + lib.Name})
				return
			}
			updated++
			continue
		}
		if model == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Choose a model on your default template first; new templates use it."})
			return
		}
		t := &models.SummaryTemplate{Name: lib.Name, Description: &desc, Prompt: lib.Prompt, Model: model,
			AutoTags: models.StringList(append([]string{}, lib.AutoTags...)), IncludeSpeakerInfo: lib.Speakers}
		if err := h.summaryRepo.Create(ctx, t); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create " + lib.Name})
			return
		}
		if lib.Name == "Default" && !hasDefault {
			_ = h.summaryRepo.SetDefaultTemplate(ctx, t.ID)
			hasDefault = true
		}
		created++
	}
	c.JSON(http.StatusOK, gin.H{"created": created, "updated": updated})
}
