package mapping

import (
	"reflect"
	"testing"
	"testing/quick"
)

func TestExtractKeywords_Deterministic(t *testing.T) {
	t.Parallel()
	prop := func(body []byte) bool {
		a := extractKeywords(body, 5)
		b := extractKeywords(body, 5)
		return reflect.DeepEqual(a, b)
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 50}); err != nil {
		t.Error(err)
	}
}

func TestExtractKeywords_RealisticDoc(t *testing.T) {
	t.Parallel()
	body := []byte(`# research-cheap

Use this skill when you need a cheap research helper. Dispatch
to gemini-flash for low-cost retrieval; never use claude-opus
for cheap tasks; cite sources verbatim.`)
	kws := extractKeywords(body, 6)
	if len(kws) == 0 {
		t.Fatal("no keywords")
	}

	again := extractKeywords(body, 6)
	if !reflect.DeepEqual(kws, again) {
		t.Errorf("non-deterministic: %v vs %v", kws, again)
	}
}

func TestExtractKeywords_EmptyInput(t *testing.T) {
	t.Parallel()
	kws := extractKeywords([]byte{}, 5)
	if kws != nil {
		t.Errorf("got %v, want nil", kws)
	}
}

func TestExtractKeywords_StopWordsDropped(t *testing.T) {
	t.Parallel()
	body := []byte("the and for with this that the and for with")
	kws := extractKeywords(body, 5)

	if len(kws) != 0 {
		t.Errorf("stop-word-only input yielded %v; want []", kws)
	}
}

func TestExtractKeywords_SmallNRespected(t *testing.T) {
	t.Parallel()
	body := []byte("alpha beta gamma delta epsilon zeta eta theta")
	kws := extractKeywords(body, 3)
	if len(kws) != 3 {
		t.Errorf("requested 3, got %d", len(kws))
	}
}

func TestExtractKeywords_LargeNGreaterThanUniqueTokens(t *testing.T) {
	t.Parallel()
	body := []byte("alpha beta gamma")
	kws := extractKeywords(body, 100)
	if len(kws) != 3 {
		t.Errorf("got %d, want 3 (all unique tokens)", len(kws))
	}
}

func TestTokenize_DropsShortTokens(t *testing.T) {
	t.Parallel()
	body := "a bc def ghij"
	got := tokenize(body)

	want := []string{"def", "ghij"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTokenize_LowercasesUnicode(t *testing.T) {
	t.Parallel()
	body := "Hola Mundo ESPAÑOL"
	got := tokenize(body)
	want := []string{"hola", "mundo", "español"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
