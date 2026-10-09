// Package pii finds and removes personal identifiers in transcripts and
// summaries: dates of birth, government ID and account numbers, card
// numbers, phone numbers, emails and street addresses. Names are not
// treated as identifiers.
package pii

import (
	"regexp"
	"sort"
	"strings"
)

// Redacted replaces an identifier in a summary.
const Redacted = "[redacted]"

const month = `(?:jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|june?|july?|aug(?:ust)?|sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)`

var (
	emailRe = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	phoneRe = regexp.MustCompile(`(?:\+?1[\s.-]?)?(?:\(\d{3}\)\s?|\b\d{3}[\s.-])\d{3}[\s.-]\d{4}\b`)
	ssnRe   = regexp.MustCompile(`\b\d{3}[- ]\d{2}[- ]\d{4}\b`)
	cardRe  = regexp.MustCompile(`\b(?:\d[ -]?){12,18}\d\b`)
	// A date shortly after a date-of-birth phrase.
	dobRe = regexp.MustCompile(`(?i)\b(date of birth|birth ?date|d\.?o\.?b\.?|born on|birthday is)(\W{0,3}(?:is|was|of)?\W{0,3})(\d{1,2}[/.-]\d{1,2}[/.-]\d{2,4}|\d{4}-\d{2}-\d{2}|` + month + `\.?\s+\d{1,2}(?:st|nd|rd|th)?,?\s+\d{4}|\d{1,2}(?:st|nd|rd|th)?\s+(?:of\s+)?` + month + `,?\s+\d{4})`)
	// Number, 1 to 3 capitalized words, street suffix: "42 Maple Ridge Road".
	// Case-sensitive so "5 minutes on the way" does not match.
	addrRe = regexp.MustCompile(`\b\d{1,6}\s+(?:[A-Z][a-z]+\s+){1,3}(?:Street|St|Avenue|Ave|Road|Rd|Drive|Dr|Lane|Ln|Court|Ct|Boulevard|Blvd|Way|Place|Pl|Circle|Cir|Trail|Terrace|Parkway|Pkwy|Highway|Hwy)\b\.?`)
	// Phrases that mean an identifier was given, even when the value was
	// spoken in words the patterns above cannot see.
	phraseRe = regexp.MustCompile(`(?i)\b(date of birth|birth ?date|social security|ssn|account number|routing number|card number|medical record number|member id|policy number|driver'?s licen[cs]e|passport number)\b`)
)

// Detect returns the kinds of identifiers found in text, sorted:
// address, card, dob, email, id, phone, ssn.
func Detect(text string) []string {
	found := map[string]bool{}
	if emailRe.MatchString(text) {
		found["email"] = true
	}
	if phoneRe.MatchString(text) {
		found["phone"] = true
	}
	if ssnRe.MatchString(text) {
		found["ssn"] = true
	}
	for _, m := range cardRe.FindAllString(text, -1) {
		if luhn(m) {
			found["card"] = true
			break
		}
	}
	if dobRe.MatchString(text) {
		found["dob"] = true
	}
	if addrRe.MatchString(text) {
		found["address"] = true
	}
	for _, m := range phraseRe.FindAllString(text, -1) {
		l := strings.ToLower(m)
		switch {
		case strings.Contains(l, "birth"):
			found["dob"] = true
		case strings.Contains(l, "social") || l == "ssn":
			found["ssn"] = true
		default:
			found["id"] = true
		}
	}
	out := make([]string, 0, len(found))
	for k := range found {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Redact replaces identifier values in text with [redacted]. The phrase
// before a date of birth is kept ("date of birth [redacted]").
func Redact(text string) string {
	text = dobRe.ReplaceAllString(text, "${1}${2}"+Redacted)
	text = emailRe.ReplaceAllString(text, Redacted)
	text = ssnRe.ReplaceAllString(text, Redacted)
	text = cardRe.ReplaceAllStringFunc(text, func(m string) string {
		if luhn(m) {
			return Redacted
		}
		return m
	})
	text = phoneRe.ReplaceAllString(text, Redacted)
	text = addrRe.ReplaceAllString(text, Redacted)
	return text
}

func luhn(s string) bool {
	sum, n, double := 0, 0, false
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c < '0' || c > '9' {
			continue
		}
		d := int(c - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		n++
		double = !double
	}
	return n >= 13 && sum%10 == 0
}
