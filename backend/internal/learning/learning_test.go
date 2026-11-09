package learning

import (
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestSummaryQuoteVerification(t *testing.T) {
	history := []Turn{{Role: "model", Content: "位置から取り出す"}, {Role: "user", Content: "ＡＢＣ、で順番に並べる。"}}
	raw := RawSummary{Understood: ptr([]RawUnderstood{
		{Point: ptr("説明した"), TurnIndex: ptr(1), Quote: ptr("ABCで順番に並べる")},
		{Point: ptr("AIを引用"), TurnIndex: ptr(0), Quote: ptr("位置から取り出す")},
		{Point: ptr("捏造"), TurnIndex: ptr(1), Quote: ptr("値を取り出せる")},
	}), Review: ptr([]RawReview{{Kind: ptr("unverified"), Text: ptr("取り出し方を確認する")}})}
	s, err := ValidateSummary(raw, history)
	if err != nil || len(s.Understood) != 1 || s.Understood[0].Evidence != "ＡＢＣ、で順番に並べる" || len(s.Review) != 1 {
		t.Fatalf("summary=%+v err=%v", s, err)
	}
	(*raw.Review)[0].Kind = ptr("unsupported")
	if _, err := ValidateSummary(raw, history); err == nil {
		t.Fatal("invalid kind accepted")
	}
}

func TestQuoteUnicodeComposition(t *testing.T) {
	got, ok := findQuote("か\u3099くしゅうする", "がくしゅうする")
	if !ok || got != "か\u3099くしゅうする" {
		t.Fatalf("%q %v", got, ok)
	}
}

func TestQuestionFormat(t *testing.T) {
	for _, question := range []string{"3.14 と Node.js はどこに出てきますか?", strings.Repeat("あ", 299) + "？"} {
		if got, err := Question("　" + question + "\n"); err != nil || got != question {
			t.Errorf("Question(%q) = %q, %v", question, got, err)
		}
	}
	for _, invalid := range []string{"？", "　?", "先に説明します. 例はありますか？", "何ですか？\u2028例はありますか?"} {
		if got, err := Question(invalid); err == nil {
			t.Errorf("Question(%q) accepted as %q", invalid, got)
		}
	}
}

func TestQuoteSkipsOversizedOccurrence(t *testing.T) {
	const quote = "値を順番に並べる"
	first := "値" + strings.Repeat("、", 200) + "を順番に並べる"
	got, ok := findQuote(first+"。"+quote+"。", quote)
	if !ok || got != quote {
		t.Fatalf("findQuote = %q, %v", got, ok)
	}
	if got, ok := findQuote(first, quote); ok {
		t.Fatalf("accepted oversized evidence %q", got)
	}
}
