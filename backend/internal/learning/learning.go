package learning

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

type Turn struct {
	Role    string `json:"role" firestore:"role"`
	Content string `json:"content" firestore:"content"`
}

type Understood struct {
	Point    string `json:"point" firestore:"point"`
	Evidence string `json:"evidence" firestore:"evidence"`
}
type Review struct {
	Kind string `json:"kind" firestore:"kind"`
	Text string `json:"text" firestore:"text"`
}
type Summary struct {
	Understood []Understood `json:"understood" firestore:"understood"`
	Review     []Review     `json:"review" firestore:"review"`
}

func (s Summary) MarshalJSON() ([]byte, error) {
	type wireSummary Summary
	if s.Understood == nil {
		s.Understood = []Understood{}
	}
	if s.Review == nil {
		s.Review = []Review{}
	}
	return json.Marshal(wireSummary(s))
}

type RawUnderstood struct {
	Point     *string `json:"point"`
	TurnIndex *int    `json:"turnIndex"`
	Quote     *string `json:"quote"`
}
type RawReview struct {
	Kind *string `json:"kind"`
	Text *string `json:"text"`
}
type RawSummary struct {
	Understood *[]RawUnderstood `json:"understood"`
	Review     *[]RawReview     `json:"review"`
}

type Generator interface {
	Start(context.Context, string) (string, error)
	Chat(context.Context, string, []Turn, string) (string, error)
	Summary(context.Context, string, []Turn) (RawSummary, error)
}

var ErrInvalidOutput = errors.New("invalid generation output")

// Allow periods inside ASCII terms such as 3.14 and Node.js.
var sentencePeriod = regexp.MustCompile(`(^|[^[:alnum:]])[.．]|[.．]($|[^[:alnum:]])`)

func Trim(s string) string { return strings.TrimFunc(s, unicode.IsSpace) }
func Length(s string) int  { return utf8.RuneCountInString(s) }

func Question(s string) (string, error) {
	s = Trim(s)
	last, size := utf8.DecodeLastRuneInString(s)
	if (last != '?' && last != '？') || Length(s) > 300 {
		return "", ErrInvalidOutput
	}
	body := s[:len(s)-size]
	if Trim(body) == "" || strings.ContainsAny(body, "?!？！。\n\r\v\f\u0085\u2028\u2029") || sentencePeriod.MatchString(body) {
		return "", ErrInvalidOutput
	}
	return s, nil
}

func ValidateSummary(raw RawSummary, history []Turn) (Summary, error) {
	if raw.Understood == nil || raw.Review == nil || len(*raw.Understood) > 3 || len(*raw.Review) > 3 {
		return Summary{}, ErrInvalidOutput
	}
	out := Summary{Understood: []Understood{}, Review: []Review{}}
	for _, item := range *raw.Understood {
		if item.Point == nil || item.Quote == nil || item.TurnIndex == nil {
			return Summary{}, ErrInvalidOutput
		}
		point, quote := Trim(*item.Point), Trim(*item.Quote)
		if point == "" || Length(point) > 80 || Length(quote) < 5 || Length(quote) > 200 {
			return Summary{}, ErrInvalidOutput
		}
		if *item.TurnIndex < 0 || *item.TurnIndex >= len(history) || history[*item.TurnIndex].Role != "user" {
			continue
		}
		if evidence, ok := findQuote(history[*item.TurnIndex].Content, quote); ok {
			out.Understood = append(out.Understood, Understood{Point: point, Evidence: evidence})
		}
	}
	for _, item := range *raw.Review {
		if item.Kind == nil || item.Text == nil {
			return Summary{}, ErrInvalidOutput
		}
		text := Trim(*item.Text)
		if (*item.Kind != "unverified" && *item.Kind != "misunderstanding") || text == "" || Length(text) > 120 {
			return Summary{}, ErrInvalidOutput
		}
		out.Review = append(out.Review, Review{Kind: *item.Kind, Text: text})
	}
	return out, nil
}

type position struct{ start, end int }

func normalized(s string) ([]rune, []position) {
	var iter norm.Iter
	iter.InitString(norm.NFKC, s)
	var chars []rune
	var positions []position
	for !iter.Done() {
		start := iter.Pos()
		segment := iter.Next()
		end := iter.Pos()
		for _, r := range string(segment) {
			if unicode.IsSpace(r) || unicode.IsPunct(r) {
				continue
			}
			chars = append(chars, r)
			positions = append(positions, position{start, end})
		}
	}
	return chars, positions
}

func findQuote(source, quote string) (string, bool) {
	haystack, positions := normalized(source)
	needle, _ := normalized(quote)
	if len(needle) < 5 || len(needle) > len(haystack) {
		return "", false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			evidence := source[positions[i].start:positions[i+len(needle)-1].end]
			if length := Length(evidence); length >= 5 && length <= 200 {
				return evidence, true
			}
		}
	}
	return "", false
}
