package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ramuthumu/gostash/internal/db"
)

func TestSentenceChunks(t *testing.T) {
	if got := sentenceChunks(""); got != nil {
		t.Errorf("empty input should give nil, got %v", got)
	}
	if got := sentenceChunks("   \n\t "); got != nil {
		t.Errorf("whitespace-only input should give nil, got %v", got)
	}

	short := "Hello world. This is a short article."
	got := sentenceChunks(short)
	if len(got) != 1 || got[0] != short {
		t.Errorf("short text should stay one chunk, got %q", got)
	}

	// Newlines must collapse into spaces, not fragment chunks.
	multi := "First sentence.\n\nSecond sentence.\nThird one!"
	got = sentenceChunks(multi)
	if len(got) != 1 || got[0] != "First sentence. Second sentence. Third one!" {
		t.Errorf("newlines should collapse, got %q", got)
	}

	// Long run with sentence punctuation: chunks break at sentence ends.
	long := ""
	for i := range 150 {
		long += "This is sentence number " + fmt.Sprint(i) + " in the article. "
	}
	got = sentenceChunks(long)
	if len(got) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(got))
	}
	for i, c := range got {
		if len(c) > 3000 {
			t.Errorf("chunk %d exceeds hard cap: %d chars", i, len(c))
		}
		if !strings.HasSuffix(strings.TrimSpace(c), ".") {
			t.Errorf("chunk %d should end at a sentence boundary: %q…", i, c[len(c)-40:])
		}
	}
	if strings.Join(got, " ") != strings.TrimSpace(long) {
		t.Errorf("chunks must reassemble to the original text")
	}

	// Punctuation-free run: hard cap split.
	wall := strings.Repeat("word ", 1500) // 7500 chars, no punctuation
	got = sentenceChunks(wall)
	if len(got) < 2 {
		t.Fatalf("punctuation-free long text should split, got %d chunks", len(got))
	}
	for _, c := range got {
		if len(c) > 3000 {
			t.Errorf("hard-cap chunk too long: %d", len(c))
		}
	}
}

func TestVoiceForText(t *testing.T) {
	def := "en-US-JennyNeural"
	if v := voiceForText("This is a plain English article.", def); v != def {
		t.Errorf("english text should use default, got %s", v)
	}
	te := "ఇది తెలుగు వ్యాసం. తెలుగులో రాయడం చాలా బాగుంది. మరిన్ని వాక్యాలు ఉన్నాయి."
	if v := voiceForText(te, def); v != "te-IN-MohanNeural" {
		t.Errorf("telugu text should use telugu voice, got %s", v)
	}
	hi := "यह एक हिंदी लेख है। हिंदी में लिखना बहुत अच्छा है। और भी वाक्य हैं।"
	if v := voiceForText(hi, def); v != "hi-IN-MadhurNeural" {
		t.Errorf("hindi text should use hindi voice, got %s", v)
	}
	// Mixed: a mostly-English paragraph with one Telugu word must stay English.
	mixed := "The festival of " + strings.Repeat("సంక్రాంతి ", 3) + "is celebrated in January."
	if v := voiceForText(mixed, def); v != def {
		t.Errorf("mostly-english text should use default, got %s", v)
	}
}

func TestXMLEscape(t *testing.T) {
	in := `Tom & Jerry <3 "quotes" 'apostrophes'`
	want := "Tom &amp; Jerry &lt;3 &quot;quotes&quot; &apos;apostrophes&apos;"
	if got := xmlEscape(in); got != want {
		t.Errorf("xmlEscape = %q, want %q", got, want)
	}
}

func TestHandleTTSStreamsChunks(t *testing.T) {
	var requests []string
	azure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		requests = append(requests, string(body[:n]))
		if got := r.Header.Get("Ocp-Apim-Subscription-Key"); got != "test-key" {
			t.Errorf("missing auth header: %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/ssml+xml" {
			t.Errorf("content-type = %q", got)
		}
		fmt.Fprintf(w, "AUDIO%d", len(requests))
	}))
	defer azure.Close()

	t.Setenv("READLATER_TTS_KEY", "test-key")
	t.Setenv("READLATER_TTS_REGION", "")
	t.Setenv("READLATER_TTS_ENDPOINT", azure.URL)

	store, err := db.Open(filepath.Join(t.TempDir(), "tts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// 300 sentences -> several chunks (sentence flushes past minChunk).
	text := ""
	for i := range 300 {
		text += fmt.Sprintf("Sentence number %d in the test article. ", i)
	}
	id, err := store.Save(db.Article{
		URL:         "https://example.com/tts-test",
		Title:       "TTS Test",
		TextContent: text,
	})
	if err != nil {
		t.Fatal(err)
	}

	s := New(store, t.TempDir())
	req := httptest.NewRequest("GET", fmt.Sprintf("/article/%d/tts", id), nil)
	req.SetPathValue("id", fmt.Sprint(id))
	rec := httptest.NewRecorder()
	s.handleTTS(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("content-type = %q, want audio/mpeg", ct)
	}
	if !strings.Contains(rec.Body.String(), "AUDIO1") || !strings.Contains(rec.Body.String(), "AUDIO2") {
		t.Errorf("body should contain streamed chunks: %q", rec.Body.String())
	}
	if len(requests) < 2 {
		t.Errorf("expected multiple azure requests, got %d", len(requests))
	}
	for _, ssml := range requests {
		if !strings.Contains(ssml, "en-US-JennyNeural") {
			t.Errorf("ssml missing default voice: %q", ssml)
		}
	}
}

func TestHandleTTSDisabledAndMissing(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "tts2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := New(store, t.TempDir())

	// No key configured -> 503.
	req := httptest.NewRequest("GET", "/article/1/tts", nil)
	rec := httptest.NewRecorder()
	s.handleTTS(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("unconfigured tts status = %d, want 503", rec.Code)
	}

	// Configured but missing article -> 404.
	t.Setenv("READLATER_TTS_KEY", "k")
	t.Setenv("READLATER_TTS_ENDPOINT", "http://127.0.0.1:1")
	s2 := New(store, t.TempDir())
	req = httptest.NewRequest("GET", "/article/999/tts", nil)
	rec = httptest.NewRecorder()
	s2.handleTTS(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing article status = %d, want 404", rec.Code)
	}
}

var _ = time.Now // keep time import if unused elsewhere
