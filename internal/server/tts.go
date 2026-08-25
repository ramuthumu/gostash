package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// TTS reads articles aloud with Azure Neural voices (natural, cross-device,
// free tier ~500k chars/month). Enabled by READLATER_TTS_KEY + REGION; when
// the key is absent the Listen button is hidden and the endpoint 503s.
type ttsConfig struct {
	Key      string
	Region   string
	Endpoint string // override (tests); defaults to <region>.tts.speech.microsoft.com
	Voice    string // default English neural voice
}

func (c ttsConfig) Enabled() bool { return c.Key != "" }

func loadTTSConfig() ttsConfig {
	cfg := ttsConfig{
		Key:    os.Getenv("READLATER_TTS_KEY"),
		Region: os.Getenv("READLATER_TTS_REGION"),
		Voice:  "en-US-JennyNeural",
	}
	if cfg.Endpoint = os.Getenv("READLATER_TTS_ENDPOINT"); cfg.Endpoint == "" && cfg.Region != "" {
		cfg.Endpoint = "https://" + cfg.Region + ".tts.speech.microsoft.com/cognitiveservices/v1"
	}
	return cfg
}

// voiceForText picks a neural voice by the dominant script: Telugu and Hindi
// articles get matching Indian voices, everything else (English, CJK, …) uses
// the default. A script wins only with enough evidence (roughly 5 words), so
// an English article quoting a few Telugu terms keeps the English voice.
func voiceForText(text string, def string) string {
	const (
		teVoice = "te-IN-MohanNeural"
		hiVoice = "hi-IN-MadhurNeural"
		need    = 40 // evidence runes needed to claim a script
	)
	telugu, devanagari, evidence := false, false, 0
	for _, r := range text {
		switch {
		case r >= 0x0C00 && r <= 0x0C7F: // Telugu block
			telugu = true
			evidence++
		case r >= 0x0900 && r <= 0x097F: // Devanagari (Hindi)
			devanagari = true
			evidence++
		}
		if evidence >= need {
			break
		}
	}
	if evidence >= need {
		if telugu {
			return teVoice
		}
		if devanagari {
			return hiVoice
		}
	}
	return def
}

// sentenceChunks splits article text into chunks that stay well under Azure's
// per-request audio limit. Chunks break at sentence punctuation once a chunk
// has grown past a minimum, and always at a hard character cap for
// punctuation-free runs, so streamed audio pauses at sentence boundaries.
func sentenceChunks(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	// Paragraph/newline breaks become spaces so they don't fragment chunks.
	text = strings.Join(strings.Fields(text), " ")

	const (
		minChunk = 120  // chars before a sentence boundary may end a chunk
		maxChunk = 3000 // hard cap: ~2-3 min of audio per chunk
	)
	var chunks []string
	start, flushAt := 0, -1
	for i, r := range text {
		if strings.ContainsRune(".!?।", r) && i+1-start >= minChunk {
			flushAt = i + 1
		}
		if i+1-start >= maxChunk {
			if flushAt < 0 {
				flushAt = i + 1
			}
			chunks = append(chunks, strings.TrimSpace(text[start:flushAt]))
			start = flushAt
			flushAt = -1
			// Consume the single separating space so chunks reassemble exactly.
			for start < len(text) && text[start] == ' ' {
				start++
			}
		}
	}
	if start < len(text) {
		chunks = append(chunks, strings.TrimSpace(text[start:]))
	}
	return chunks
}

// xmlEscape makes article text safe inside SSML.
func xmlEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ttsServer memoizes config; guarded because ListenAndServe may spawn worker
// goroutines that touch it.
type ttsServer struct {
	once sync.Once
	cfg  ttsConfig
}

func (t *ttsServer) config() ttsConfig {
	t.once.Do(func() { t.cfg = loadTTSConfig() })
	return t.cfg
}

// synthChunk runs one Azure request and returns the complete mp3 bytes.
func synthChunk(cfg ttsConfig, chunk, voice string) ([]byte, error) {
	ssml := fmt.Sprintf(
		`<speak version="1.0" xmlns="http://www.w3.org/2001/10/synthesis" xml:lang="en-US"><voice name="%s"><prosody rate="+0%%">%s</prosody></voice></speak>`,
		voice, xmlEscape(chunk),
	)
	req, err := http.NewRequest(http.MethodPost, cfg.Endpoint, bytes.NewReader([]byte(ssml)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/ssml+xml")
	req.Header.Set("X-Microsoft-OutputFormat", "audio-24khz-48kbitrate-mono-mp3")
	req.Header.Set("Ocp-Apim-Subscription-Key", cfg.Key)
	req.Header.Set("User-Agent", "gostash/1.0")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("azure tts: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return io.ReadAll(resp.Body)
}
