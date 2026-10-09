package titles

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Flag and source tags added by code, never chosen by the model.
const (
	TagSensitive = "sensitive"
	TagPII       = "pii"
	TagYouTube   = "youtube"
	TypeMedia    = "media"

	maxTopics = 3
)

// DefaultTypes is the built-in list of recording types, one per line as
// "tag: description". A leading ! marks the type as sensitive.
const DefaultTypes = `!individual therapy: one client with a therapist or counselor
!couples therapy: two partners together with a therapist or counselor
!relationship talk: partners talking to each other about their own relationship (needs, trust, conflict, their future), no therapist present
conversation: any other talk between family, friends, acquaintances or partners, including partners talking about family, plans or daily life
!medical appointment: a visit or call with a doctor, nurse, specialist or speech, voice or physical therapist
!advisor meeting: a lawyer, mediator, accountant, financial advisor, realtor or other paid professional
work meeting: colleagues, clients, vendors or interviews about work
personal note: one person talking alone: voice memo, journal, reminder or reflection
media: recorded content made for an audience: video, podcast, lecture, audiobook or guided meditation`

// DefaultTopics is the built-in list of topics, in the same format.
const DefaultTopics = `relationships: a romantic relationship between partners: connection, needs, trust, intimacy, commitment. Not for family or friends
!separation: separating or divorce: moving out, dividing finances or property, co-parenting arrangements
conflict: an argument or heated disagreement happens in the recording itself
family: parents, siblings, extended family, family history and how a family gets along
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
// per line: "old, other old = vocabulary tag". Rewordings only: specific
// details (a condition, a practice, a test) stay as keywords.
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
stress, mentalhealth, mental illness = mental health
loss, bereavement, death = grief
self-improvement, self development = personal growth
mindfulness practice = mindfulness
symptoms, sleep, diet, neurological = health
swallowing, voice, vocal function, throat = voice and swallowing
lab results, diagnostic, test results = medical tests
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
	// NameHints maps words in a recording's own name to a type, which then
	// wins over the model: "therapy session" means individual therapy.
	NameHints map[string]string
	// TitlePrefixes maps a type to a label for generated titles.
	TitlePrefixes map[string]string
}

// DefaultTitlePrefixes puts a label in front of the topic in generated
// titles, one rule per line: "type = Label". "2026-06-03 Therapy: ...".
const DefaultTitlePrefixes = `individual therapy = Therapy
couples therapy = Couples Therapy
medical appointment = Medical
advisor meeting = Advisor`

// WithTitlePrefixes sets the title labels from settings text; empty means
// the default.
func (v Vocabulary) WithTitlePrefixes(text string) Vocabulary {
	if strings.TrimSpace(text) == "" {
		text = DefaultTitlePrefixes
	}
	v.TitlePrefixes = map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		left, right, ok := strings.Cut(line, "=")
		label := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(right), ":"))
		if t := v.ResolveType(left); ok && t != "" && label != "" {
			v.TitlePrefixes[t] = label
		}
	}
	return v
}

// PrefixTopic puts the type's title label in front of topic ("Therapy:
// Boundaries With Family"), unless the topic already starts with it.
func (v Vocabulary) PrefixTopic(typ, topic string) string {
	label := v.TitlePrefixes[typ]
	if label == "" || strings.HasPrefix(strings.ToLower(topic), strings.ToLower(label)) {
		return topic
	}
	return label + ": " + topic
}

// DefaultNameHints sets the type from words in the name the person gave
// the file, one rule per line: "words, other words = type". The longest
// match wins, so "speech therapy" beats "therapy".
const DefaultNameHints = `couples therapy, couples session, couples counseling, marriage counseling = couples therapy
therapy, therapy session, therapist, counseling, counselling = individual therapy
speech therapy, voice therapy, physical therapy, doctor, appointment, neurologist, ent, clinic = medical appointment
lawyer, attorney, mediator, mediation, accountant, financial advisor = advisor meeting
meeting, standup, one on one, interview = work meeting
voice memo, journal, note to self = personal note`

// WithNameHints sets the name hints from settings text; empty means the
// default.
func (v Vocabulary) WithNameHints(text string) Vocabulary {
	if strings.TrimSpace(text) == "" {
		text = DefaultNameHints
	}
	v.NameHints = map[string]string{}
	for phrase, typ := range ParseSynonyms(text) {
		if t := v.ResolveType(typ); t != "" {
			v.NameHints[phrase] = t
		}
	}
	return v
}

func nameWords(s string) []string {
	return strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, s))
}

// TypeFromName returns the type the recording's own name points to, or "".
// Words match whole, ignoring case, punctuation and plurals; the longest
// matching phrase wins.
func (v Vocabulary) TypeFromName(name string) string {
	words := nameWords(name)
	best, bestLen := "", 0
	for phrase, typ := range v.NameHints {
		p := nameWords(phrase)
		if len(p) == 0 || len(p) > len(words) {
			continue
		}
		for i := 0; i+len(p) <= len(words); i++ {
			ok := true
			for j := range p {
				if !sameWord(p[j], words[i+j]) {
					ok = false
					break
				}
			}
			if ok && (len(phrase) > bestLen || (len(phrase) == bestLen && typ < best)) {
				best, bestLen = typ, len(phrase)
			}
		}
	}
	return best
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

// Keywords are free-form tags next to the fixed type and topics.
const (
	DefaultKeywords  = 2
	MaxKeywords      = 5
	maxKnownKeywords = 40
)

// IsFlag reports whether tag is one Scriberr sets itself as a flag rather
// than a tag.
func IsFlag(tag string) bool { return tag == TagSensitive || tag == TagPII }

// StrictPrompt asks for a topic, brief, one type, topics and up to
// keywords free-form tags. PII is not asked for: the model flagged it from
// summary wording alone and was wrong far more often than right, so it
// comes from the transcript check only. name is the recording's current
// title or file name, a useful hint. known lists keywords already in use,
// most used first, for reuse.
func (v Vocabulary) StrictPrompt(summary, name, opening, knownType string, keywords int, known []string) string {
	var b strings.Builder
	shape := `{"topic": "...", "brief": "...", "type": "...", "topics": ["..."]}`
	if keywords > 0 {
		shape = `{"topic": "...", "brief": "...", "type": "...", "topics": ["..."], "keywords": ["..."]}`
	}
	b.WriteString("Read this summary of a recording and return JSON only, no other text:\n" + shape + `

topic: 4 to 8 words naming what the recording is specifically about. Title Case. No date, no quotes, no trailing punctuation. Avoid generic words like Recording, Conversation, Discussion, Meeting, Summary.
brief: one plain sentence of at most 25 words saying what the recording covers, for a list view. Describe the setting correctly: in a therapy session the speakers are a client and a therapist, so write "therapy session", never "partners" or "two individuals"; people who are only talked about are not speakers. Never include a date of birth, ID or account number, phone number, email or address.
type: exactly one recording type from the list below, spelled exactly as shown. Decide by who is talking and the setting, not by the subject: a therapist or counselor taking part makes it a therapy type even when the subject is a relationship. The recording's name and the opening of the transcript below are the best evidence.
topics: 2 or 3 topics from the list below, spelled exactly as shown, most important first. Use only 1 when the recording is very short or about a single thing. Only main subjects, not passing mentions. Never invent a topic.
`)
	if keywords > 0 {
		b.WriteString(fmt.Sprintf(`keywords: 1 to %d specific lowercase tags of 1 or 2 words for concrete details central to the recording (not passing mentions) that the lists do not cover: a named condition, medication, test or procedure, a practice or method, an event, a project, a place or an object that matters. Concrete nouns only. Never feelings, moods, behaviors or traits (not "uncertainty", "defensive", "resilience"): the topics and summary cover those. Not a recording type or topic, not a person's name, not a date or number, and no diagnosis nobody stated. Use none when nothing concrete stands out.
`, keywords))
		if len(known) > maxKnownKeywords {
			known = known[:maxKnownKeywords]
		}
		if len(known) > 0 {
			b.WriteString("Keywords already in use (reuse one, spelled exactly, when it fits): " + strings.Join(known, ", ") + "\n")
		}
	}
	b.WriteString("\nRecording types:\n")
	for _, e := range v.Types {
		b.WriteString("- " + e.Tag + ": " + e.Description + "\n")
	}
	b.WriteString("\nTopics:\n")
	for _, e := range v.Topics {
		b.WriteString("- " + e.Tag + ": " + e.Description + "\n")
	}
	if knownType != "" {
		desc := ""
		for _, e := range v.Types {
			if e.Tag == knownType {
				desc = " (" + e.Description + ")"
			}
		}
		b.WriteString("\nThe recording type is already known from its name: " + knownType + desc + ". Use it as \"type\", and write the topic and brief to match who is actually speaking in that setting.\n")
	}
	if name = strings.TrimSpace(name); name != "" {
		b.WriteString("\nName the person who recorded it gave the file or recording: " + name + "\nWords in it such as therapy, counseling, session, doctor, appointment, meeting or interview are strong evidence of the type.\n")
	}
	if opening = strings.TrimSpace(opening); opening != "" {
		b.WriteString("\nOpening of the transcript (to judge the setting and who is talking):\n" + opening + "\n")
	}
	b.WriteString("\nSummary:\n")
	b.WriteString(summary)
	return b.String()
}

// Classified is the model's reply in strict mode.
type Classified struct {
	Topic    string
	Brief    string
	Type     string
	Topics   []string
	Keywords []string
	PII      bool
}

// ParseStrict reads a reply to StrictPrompt. Type and topics are mapped
// onto the vocabulary; anything else from those fields is dropped.
// Keywords that turn out to be a type or topic are moved there. Replies
// that put everything in "tags" are sorted the same way, with leftovers
// kept as keywords.
func (v Vocabulary) ParseStrict(raw string) (Classified, error) {
	raw = stripThinking(raw)
	m := jsonObjRe.FindString(raw)
	if m == "" {
		return Classified{}, errors.New("no JSON object in reply")
	}
	var r struct {
		Topic    string          `json:"topic"`
		Title    string          `json:"title"`
		Brief    string          `json:"brief"`
		Type     string          `json:"type"`
		Topics   []string        `json:"topics"`
		Keywords []string        `json:"keywords"`
		Tags     []string        `json:"tags"`
		PII      json.RawMessage `json:"pii"`
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
	addTopic := func(tp string) bool {
		if tp == "" || seen[tp] {
			return tp != ""
		}
		if len(out.Topics) < maxTopics {
			seen[tp] = true
			out.Topics = append(out.Topics, tp)
		}
		return true
	}
	takeTypeOrTopic := func(t string) bool {
		if typ := v.ResolveType(t); typ != "" {
			if out.Type == "" {
				out.Type = typ
			}
			return true
		}
		return addTopic(v.ResolveTopic(t))
	}
	for _, t := range r.Topics {
		takeTypeOrTopic(t)
	}
	var free []string
	for _, t := range append(append([]string{}, r.Keywords...), r.Tags...) {
		if !takeTypeOrTopic(t) {
			free = append(free, t)
		}
	}
	if out.Type == "" {
		// A topic named as the type ("type": "finances") still counts.
		addTopic(v.ResolveTopic(r.Type))
	}
	kw := map[string]bool{}
	for _, t := range free {
		t = NormalizeTag(t)
		if t == "" || len(t) > maxTagLen || len(strings.Fields(t)) > 3 || IsFlag(t) || t == TagYouTube || kw[t] {
			continue
		}
		kw[t] = true
		out.Keywords = append(out.Keywords, t)
	}
	return out, nil
}

// Flags are facts known outside the model.
type Flags struct {
	YouTube  bool   // the audio came from YouTube
	PII      bool   // identifiers found in the transcript
	NameType string // type set by the recording's own name (TypeFromName)
}

// Assemble builds the tags and the flags. Tags: type, topics, up to
// keywords free-form keywords (folded onto known ones), then youtube.
// Flags, kept apart and not counted as tags: sensitive when the type or a
// topic is marked sensitive or identifiers were found, and pii when the
// transcript check found identifiers. YouTube recordings are always the
// media type.
func (v Vocabulary) Assemble(c Classified, f Flags, keywords int, known []string) (tags, flags []string) {
	typ := c.Type
	if f.NameType != "" {
		typ = f.NameType
	}
	if f.YouTube {
		if t := v.ResolveType(TypeMedia); t != "" {
			typ = t
		}
	}
	sensitive := false
	if typ != "" {
		tags = append(tags, typ)
		sensitive = v.sensitive(typ)
	}
	for _, t := range c.Topics {
		tags = append(tags, t)
		sensitive = sensitive || v.sensitive(t)
	}
	if keywords > MaxKeywords {
		keywords = MaxKeywords
	}
	if keywords > 0 {
		has := map[string]bool{}
		for _, t := range tags {
			has[t] = true
		}
		n := 0
		for _, k := range Standardize(foldStems(c.Keywords, known), known, len(c.Keywords)) {
			if has[k] || n == keywords {
				continue
			}
			has[k] = true
			tags = append(tags, k)
			n++
		}
	}
	if f.YouTube {
		tags = append(tags, TagYouTube)
	}
	// PII comes from the transcript check only; c.PII (the model's opinion)
	// is parsed for older replies but ignored.
	if sensitive || f.PII {
		flags = append(flags, TagSensitive)
	}
	if f.PII {
		flags = append(flags, TagPII)
	}
	return tags, flags
}

// SplitFlags separates free-form tags from flags and adds youtube and the
// flags known outside the model (the non-strict mode).
func SplitFlags(in []string, f Flags) (tags, flags []string) {
	has := map[string]bool{}
	for _, t := range in {
		if IsFlag(t) || has[t] {
			continue
		}
		has[t] = true
		tags = append(tags, t)
	}
	if f.YouTube && !has[TagYouTube] {
		tags = append(tags, TagYouTube)
	}
	if f.PII {
		flags = []string{TagSensitive, TagPII}
	}
	return tags, flags
}

// stemKey reduces a keyword to a rough stem per word, so "defensive" and
// "defensiveness", or "insular" and "insularity", compare equal.
func stemKey(tag string) string {
	words := strings.Fields(NormalizeTag(tag))
	for i, w := range words {
		for _, suf := range []string{"iveness", "ness", "ity", "ive", "ing", "ed", "es", "s"} {
			if strings.HasSuffix(w, suf) && len(w)-len(suf) >= 4 {
				w = strings.TrimSuffix(w, suf)
				break
			}
		}
		words[i] = w
	}
	return strings.Join(words, " ")
}

// foldStems replaces each keyword with a known keyword of the same stem,
// and drops later keywords whose stem repeats an earlier one.
func foldStems(keywords, known []string) []string {
	byStem := map[string]string{}
	for _, k := range known {
		if s := stemKey(k); s != "" {
			if _, ok := byStem[s]; !ok {
				byStem[s] = k
			}
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, k := range keywords {
		s := stemKey(k)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		if kk, ok := byStem[s]; ok {
			k = kk
		}
		out = append(out, k)
	}
	return out
}
