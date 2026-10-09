package titles

import (
	"encoding/json"
	"errors"
	"strings"
)

// Flag and source tags added by code, never chosen by the model.
const (
	TagSensitive = "sensitive"
	TagPII       = "pii"
	TagYouTube   = "youtube"
	TypeMedia    = "media"

	maxTopics     = 3
	maxStrictTags = 7
)

// DefaultTypes is the built-in list of recording types, one per line as
// "tag: description". A leading ! marks the type as sensitive.
const DefaultTypes = `!individual therapy: one client with a therapist or counselor
!couples therapy: two partners together with a therapist or counselor
!relationship talk: partners talking to each other about their relationship, no therapist present
conversation: any other talk between family, friends or acquaintances
!medical appointment: a visit or call with a doctor, nurse, specialist or speech, voice or physical therapist
!advisor meeting: a lawyer, mediator, accountant, financial advisor, realtor or other paid professional
work meeting: colleagues, clients, vendors or interviews about work
personal note: one person talking alone: voice memo, journal, reminder or reflection
media: recorded content made for an audience: video, podcast, lecture, audiobook or guided meditation`

// DefaultTopics is the built-in list of topics, in the same format.
const DefaultTopics = `relationships: a romantic relationship: connection, needs, trust, intimacy, commitment
!separation: separating or divorce: moving out, dividing finances or property, co-parenting arrangements
conflict: an argument or heated disagreement happens in the recording itself
family: parents, siblings, extended family and family history
parenting: raising children, their needs, school and activities
caregiving: caring for a sick, aging or disabled person or pet
!mental health: anxiety, depression, ADHD, bipolar disorder, stress or other mental health conditions
!grief: death, loss and mourning
!trauma: past trauma and its effects
personal growth: self-acceptance, habits, values, philosophy and self-improvement
mindfulness: meditation, breathing, journaling and similar practices
!health: physical symptoms, conditions, diet, sleep and general health
!voice and swallowing: voice, throat, swallowing and reflux problems
!medical tests: lab work, scans, imaging and test results
!medication and treatment: prescriptions, therapy plans, exercises and procedures
!finances: money, budgets, debts, accounts, transfers and spending
!legal: contracts, agreements, legal rights, courts and lawyers
housing: home, moving, rent, mortgage and repairs
work: jobs, career, workplace stress, business deals and company decisions
technology: devices, software, setup and troubleshooting
school: classes, grades, teachers and student activities
pets: animals and their care
travel: trips, vacations and travel planning`

// DefaultSynonyms maps common model wording onto the vocabulary, one rule
// per line: "old, other old = vocabulary tag".
const DefaultSynonyms = `therapy session, therapy, counseling, counselling, individual counseling = individual therapy
couples counseling, couples session, marriage counseling = couples therapy
relationship conversation, relationship discussion, couple conversation = relationship talk
doctor visit, medical consultation, medical, speech therapy, voice therapy = medical appointment
meeting, business meeting, team meeting = work meeting
voice memo, journal, reflection, monologue = personal note
video, podcast, lecture, youtube, audiobook, guided meditation = media
marriage, attachment, trust, trust issues, intimacy, relationship tensions = relationships
divorce, co-parenting, moving out = separation
argument, fight, disagreement, relationship conflict = conflict
family dynamics, family relationships = family
kids, children, child, parenting challenges = parenting
anxiety, depression, adhd, bipolar, bipolar disorder, stress, mentalhealth = mental health
loss, bereavement, death = grief
self-acceptance, self-compassion, stoicism, self-improvement = personal growth
meditation, breathing, journaling = mindfulness
symptoms, sleep, diet, neurological = health
swallowing, voice, vocal function, throat, acid reflux, reflux, hoarseness = voice and swallowing
blood tests, imaging, brain scan, radiology, lab results, diagnostic = medical tests
medication, treatment, treatment options, treatment planning, home exercises, referrals = medication and treatment
financial, money, budget, financial responsibilities, financial trust = finances
contract, legal issues = legal
house, home, moving = housing
business, job, career, work stress, job loss = work
phones, devices, software = technology
education, class rankings = school
dog, dog health, cat = pets
trip, vacation = travel`

// Entry is one vocabulary tag.
type Entry struct {
	Tag         string `json:"tag"`
	Description string `json:"description"`
	Sensitive   bool   `json:"sensitive"`
}

// Vocabulary is the fixed set of tags the model chooses from.
type Vocabulary struct {
	Types    []Entry
	Topics   []Entry
	Synonyms map[string]string
}

// ParseEntries reads "[!]tag: description" lines. Blank lines and lines
// starting with # are ignored.
func ParseEntries(text string) []Entry {
	var out []Entry
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		e := Entry{}
		if strings.HasPrefix(line, "!") {
			e.Sensitive = true
			line = strings.TrimSpace(line[1:])
		}
		tag, desc, _ := strings.Cut(line, ":")
		e.Tag = NormalizeTag(tag)
		e.Description = strings.TrimSpace(desc)
		if e.Tag == "" || len(e.Tag) > maxTagLen || seen[e.Tag] {
			continue
		}
		seen[e.Tag] = true
		out = append(out, e)
	}
	return out
}

// ParseSynonyms reads "old, other old = tag" lines.
func ParseSynonyms(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		left, right, ok := strings.Cut(line, "=")
		target := NormalizeTag(right)
		if !ok || target == "" {
			continue
		}
		for _, old := range strings.Split(left, ",") {
			if o := NormalizeTag(old); o != "" {
				out[o] = target
			}
		}
	}
	return out
}

// NewVocabulary builds a vocabulary from the settings text; empty text
// means the built-in default.
func NewVocabulary(types, topics, synonyms string) Vocabulary {
	if strings.TrimSpace(types) == "" {
		types = DefaultTypes
	}
	if strings.TrimSpace(topics) == "" {
		topics = DefaultTopics
	}
	if strings.TrimSpace(synonyms) == "" {
		synonyms = DefaultSynonyms
	}
	return Vocabulary{Types: ParseEntries(types), Topics: ParseEntries(topics), Synonyms: ParseSynonyms(synonyms)}
}

// resolve maps a tag the model wrote onto one of entries, or "".
func (v Vocabulary) resolve(tag string, entries []Entry) string {
	tag = NormalizeTag(tag)
	if tag == "" {
		return ""
	}
	candidates := []string{tag, strings.ReplaceAll(tag, "-", " ")}
	candidates = append(candidates, variants(tag)...)
	for _, c := range candidates {
		if s, ok := v.Synonyms[c]; ok {
			candidates = append(candidates, s)
		}
	}
	for _, c := range candidates {
		for _, e := range entries {
			if e.Tag == c {
				return e.Tag
			}
		}
	}
	return ""
}

// ResolveType returns the vocabulary type for tag, or "".
func (v Vocabulary) ResolveType(tag string) string { return v.resolve(tag, v.Types) }

// ResolveTopic returns the vocabulary topic for tag, or "".
func (v Vocabulary) ResolveTopic(tag string) string { return v.resolve(tag, v.Topics) }

func (v Vocabulary) sensitive(tag string) bool {
	for _, list := range [][]Entry{v.Types, v.Topics} {
		for _, e := range list {
			if e.Tag == tag {
				return e.Sensitive
			}
		}
	}
	return false
}

// StrictPrompt asks for a topic, brief, one type and topics. PII is not
// asked for: the model flagged it from summary wording alone and was wrong
// far more often than right, so it comes from the transcript check only.
// name is the recording's current title or file name, a useful hint.
func (v Vocabulary) StrictPrompt(summary, name string) string {
	var b strings.Builder
	b.WriteString(`Read this summary of a recording and return JSON only, no other text:
{"topic": "...", "brief": "...", "type": "...", "topics": ["..."]}

topic: 4 to 8 words naming what the recording is specifically about. Title Case. No date, no quotes, no trailing punctuation. Avoid generic words like Recording, Conversation, Discussion, Meeting, Summary.
brief: one plain sentence of at most 25 words saying what the recording covers, for a list view. Never include a date of birth, ID or account number, phone number, email or address.
type: exactly one recording type from the list below, spelled exactly as shown. Decide by who is talking and the setting, not by the subject.
topics: 1 to 3 topics from the list below, spelled exactly as shown, most important first. Only main subjects, not passing mentions. Never invent a topic.

Recording types:
`)
	for _, e := range v.Types {
		b.WriteString("- " + e.Tag + ": " + e.Description + "\n")
	}
	b.WriteString("\nTopics:\n")
	for _, e := range v.Topics {
		b.WriteString("- " + e.Tag + ": " + e.Description + "\n")
	}
	if name = strings.TrimSpace(name); name != "" {
		b.WriteString("\nFile name or title (a hint, may be generic): " + name + "\n")
	}
	b.WriteString("\nSummary:\n")
	b.WriteString(summary)
	return b.String()
}

// Classified is the model's reply in strict mode.
type Classified struct {
	Topic  string
	Brief  string
	Type   string
	Topics []string
	PII    bool
}

// ParseStrict reads a reply to StrictPrompt. Type and topics are mapped
// onto the vocabulary; anything else is dropped. Replies that put
// everything in "tags" are sorted into type and topics.
func (v Vocabulary) ParseStrict(raw string) (Classified, error) {
	raw = stripThinking(raw)
	m := jsonObjRe.FindString(raw)
	if m == "" {
		return Classified{}, errors.New("no JSON object in reply")
	}
	var r struct {
		Topic  string          `json:"topic"`
		Title  string          `json:"title"`
		Brief  string          `json:"brief"`
		Type   string          `json:"type"`
		Topics []string        `json:"topics"`
		Tags   []string        `json:"tags"`
		PII    json.RawMessage `json:"pii"`
	}
	if err := json.Unmarshal([]byte(m), &r); err != nil {
		return Classified{}, err
	}
	if r.Topic == "" {
		r.Topic = r.Title
	}
	out := Classified{Topic: cleanTopic(r.Topic), Brief: cleanBrief(r.Brief)}
	if out.Topic == "" {
		return Classified{}, errors.New("empty topic")
	}
	pii := strings.ToLower(strings.Trim(strings.TrimSpace(string(r.PII)), `"`))
	out.PII = pii == "true" || pii == "yes"

	out.Type = v.ResolveType(r.Type)
	seen := map[string]bool{}
	for _, t := range append(append([]string{}, r.Topics...), r.Tags...) {
		if out.Type == "" {
			if typ := v.ResolveType(t); typ != "" {
				out.Type = typ
				continue
			}
		}
		if tp := v.ResolveTopic(t); tp != "" && !seen[tp] && len(out.Topics) < maxTopics {
			seen[tp] = true
			out.Topics = append(out.Topics, tp)
		}
	}
	if out.Type == "" {
		// A topic named as the type ("type": "finances") still counts.
		if tp := v.ResolveTopic(r.Type); tp != "" && !seen[tp] && len(out.Topics) < maxTopics {
			out.Topics = append(out.Topics, tp)
		}
	}
	return out, nil
}

// Flags are facts known outside the model.
type Flags struct {
	YouTube bool // the audio came from YouTube
	PII     bool // identifiers found in the transcript
}

// Tags assembles the final tags: type, topics, then youtube, sensitive
// and pii. YouTube recordings are always the media type. A recording is
// sensitive when its type or a topic is marked sensitive, or when the
// transcript check found identifiers (pii).
func (v Vocabulary) Tags(c Classified, f Flags) []string {
	var out []string
	typ := c.Type
	if f.YouTube {
		if t := v.ResolveType(TypeMedia); t != "" {
			typ = t
		}
	}
	sensitive := false
	if typ != "" {
		out = append(out, typ)
		sensitive = v.sensitive(typ)
	}
	for _, t := range c.Topics {
		out = append(out, t)
		sensitive = sensitive || v.sensitive(t)
	}
	if f.YouTube {
		out = append(out, TagYouTube)
	}
	// PII comes from the transcript check only; c.PII (the model's opinion)
	// is parsed for older replies but ignored.
	pii := f.PII
	if sensitive || pii {
		out = append(out, TagSensitive)
	}
	if pii {
		out = append(out, TagPII)
	}
	if len(out) > maxStrictTags {
		out = out[:maxStrictTags]
	}
	return out
}

// WithFlags adds youtube, sensitive and pii to free-form tags.
func WithFlags(tags []string, f Flags) []string {
	out := append([]string{}, tags...)
	has := map[string]bool{}
	for _, t := range out {
		has[t] = true
	}
	add := func(t string) {
		if !has[t] {
			has[t] = true
			out = append(out, t)
		}
	}
	if f.YouTube {
		add(TagYouTube)
	}
	if f.PII {
		add(TagSensitive)
		add(TagPII)
	}
	return out
}
