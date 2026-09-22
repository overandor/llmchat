// llmchat — provider-agnostic terminal chat. One static binary, zero deps.
// Providers: ollama | openai (any /v1/chat/completions endpoint incl. llm-proxy,
// groq, together, openrouter) | anthropic.
// Config: LLM_PROVIDER LLM_ENDPOINT LLM_API_KEY LLM_MODEL (or flags).
//
//	llmchat -provider ollama -model qwen2.5:0.5b
//	LLM_ENDPOINT=https://api.groq.com/openai/v1 LLM_API_KEY=gsk_… LLM_MODEL=llama-3.3-70b llmchat -provider openai
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	provider = flag.String("provider", envOr("LLM_PROVIDER", "ollama"), "ollama|openai|anthropic")
	endpoint = flag.String("endpoint", envOr("LLM_ENDPOINT", ""), "API base URL")
	apiKey   = flag.String("key", envOr("LLM_API_KEY", ""), "API key")
	model    = flag.String("model", envOr("LLM_MODEL", ""), "model name")
	system   = flag.String("system", envOr("LLM_SYSTEM", ""), "system prompt")
	temp     = flag.Float64("temp", 0.7, "temperature")
)

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

type Msg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

var hist []Msg

func main() {
	flag.Parse()
	switch *provider {
	case "ollama":
		if *endpoint == "" {
			*endpoint = "http://localhost:11434"
		}
		if *model == "" {
			*model = detectOllama()
		}
	case "openai":
		if *endpoint == "" {
			*endpoint = "https://api.openai.com"
		}
		if *model == "" {
			*model = "gpt-4o-mini"
		}
	case "anthropic":
		if *endpoint == "" {
			*endpoint = "https://api.anthropic.com"
		}
		if *model == "" {
			*model = "claude-sonnet-4-5-20250929"
		}
	}
	if *system != "" {
		hist = append(hist, Msg{"system", *system})
	}
	fmt.Printf("llmchat · %s @ %s · model %s\n", *provider, *endpoint, *model)
	fmt.Println("/help for commands · empty line + ctrl-d to quit")
	repl()
}

func detectOllama() string {
	var out struct {
		Models []struct{ Name string } `json:"models"`
	}
	if get(*endpoint+"/api/tags", nil, &out) == nil && len(out.Models) > 0 {
		return out.Models[0].Name
	}
	return "qwen2.5:0.5b"
}

func get(url string, hdr map[string]string, v any) error {
	req, _ := http.NewRequest("GET", url, nil)
	for k, x := range hdr {
		req.Header.Set(k, x)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// ── streaming ──
// stream reads SSE lines and calls fn per content delta.
func stream(req *http.Request, fn func(string)) error {
	r, err := (&http.Client{Timeout: 0}).Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		b, _ := io.ReadAll(r.Body)
		return fmt.Errorf("HTTP %d: %.200s", r.StatusCode, b)
	}
	sc := bufio.NewScanner(r.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var line struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"` // ollama native
		Delta struct {
			Text string `json:"text"`
		} `json:"delta"` // anthropic
		Type string `json:"type"`
	}
	for sc.Scan() {
		l := sc.Text()
		if !strings.HasPrefix(l, "data:") && !strings.HasPrefix(l, "{") {
			continue
		}
		l = strings.TrimPrefix(l, "data:")
		l = strings.TrimSpace(l)
		if l == "[DONE]" || l == "" {
			continue
		}
		line.Choices, line.Message, line.Delta, line.Type = nil, struct {
			Content string `json:"content"`
		}{}, struct {
			Text string `json:"text"`
		}{}, ""
		if json.Unmarshal([]byte(l), &line) != nil {
			continue
		}
		var tok string
		switch {
		case len(line.Choices) > 0:
			tok = line.Choices[0].Delta.Content
		case line.Type == "content_block_delta":
			tok = line.Delta.Text
		default:
			tok = line.Message.Content
		}
		if tok != "" {
			fn(tok)
		}
	}
	return sc.Err()
}

// ── providers ──
func chat() (string, error) {
	var req *http.Request
	var err error
	switch *provider {
	case "ollama":
		body, _ := json.Marshal(map[string]any{"model": *model, "messages": hist, "stream": true, "options": map[string]any{"temperature": *temp}})
		req, err = http.NewRequest("POST", *endpoint+"/api/chat", bytes.NewReader(body))
	case "anthropic":
		msgs := hist
		sys := ""
		if len(msgs) > 0 && msgs[0].Role == "system" {
			sys, msgs = msgs[0].Content, msgs[1:]
		}
		body, _ := json.Marshal(map[string]any{"model": *model, "messages": msgs, "system": sys, "max_tokens": 4096, "temperature": *temp, "stream": true})
		req, err = http.NewRequest("POST", *endpoint+"/v1/messages", bytes.NewReader(body))
		req.Header.Set("x-api-key", *apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	default: // openai-compatible
		body, _ := json.Marshal(map[string]any{"model": *model, "messages": hist, "temperature": *temp, "stream": true})
		req, err = http.NewRequest("POST", *endpoint+"/v1/chat/completions", bytes.NewReader(body))
		if *apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+*apiKey)
		}
	}
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	var sb strings.Builder
	err = stream(req, func(t string) { fmt.Print(t); sb.WriteString(t) })
	fmt.Println()
	return sb.String(), err
}

// ── repl ──
func repl() {
	rd := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("\033[1;32m▸\033[0m ")
		var in strings.Builder
		var queued string // a /command swallowed as message tail gets re-dispatched
		for {             // multiline until empty line or a /command line
			l, err := rd.ReadString('\n')
			if err != nil {
				fmt.Println()
				return
			}
			if l == "\n" {
				break
			}
			if strings.HasPrefix(l, "/") {
				queued = l
				break
			}
			in.WriteString(l)
		}
		line := strings.TrimSpace(in.String())
		if line != "" {
			hist = append(hist, Msg{"user", line})
			fmt.Print("\033[1;34m")
			out, err := chat()
			fmt.Print("\033[0m")
			if err != nil {
				fmt.Println("error:", err)
				hist = hist[:len(hist)-1] // don't poison context on failure
			} else {
				hist = append(hist, Msg{"assistant", out})
			}
		}
		if queued != "" {
			q := queued
			queued = ""
			if cmd(strings.TrimSpace(q)) {
				return
			}
		}
	}
}

func cmd(l string) bool {
	f := strings.Fields(l)
	switch f[0] {
	case "/exit", "/quit":
		return true
	case "/clear":
		hist = hist[:0]
		if *system != "" {
			hist = append(hist, Msg{"system", *system})
		}
		fmt.Println("(context cleared)")
	case "/model":
		if len(f) > 1 {
			*model = f[1]
		}
		fmt.Println("model:", *model)
	case "/provider":
		if len(f) > 1 {
			*provider = f[1]
		}
		fmt.Println("provider:", *provider, "@", *endpoint)
	case "/system":
		*system = strings.TrimPrefix(l, "/system ")
		if len(hist) > 0 && hist[0].Role == "system" {
			hist[0].Content = *system
		} else {
			hist = append([]Msg{{"system", *system}}, hist...)
		}
	case "/models":
		if *provider == "ollama" {
			var o struct {
				Models []struct {
					Name string
				} `json:"models"`
			}
			get(*endpoint+"/api/tags", nil, &o)
			for _, m := range o.Models {
				fmt.Println(" ", m.Name)
			}
		} else {
			fmt.Println("(list unsupported for this provider)")
		}
	case "/save":
		name := "chat-" + time.Now().Format("20060102-150405") + ".md"
		if len(f) > 1 {
			name = f[1]
		}
		var b strings.Builder
		for _, m := range hist {
			b.WriteString("**" + m.Role + ":** " + m.Content + "\n\n")
		}
		os.WriteFile(filepath.Clean(name), []byte(b.String()), 0o644)
		fmt.Println("saved", name)
	case "/help":
		fmt.Println("/model /models /provider /system /clear /save /exit · enter sends · empty line ends multiline")
	default:
		fmt.Println("unknown:", f[0])
	}
	return false
}
