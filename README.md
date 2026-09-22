# llmchat

Provider-agnostic LLM chat for the terminal. One static binary, zero dependencies — drops onto any Ubuntu box (x86_64 or arm64) and turns the shell into a chat client.

## Install (Ubuntu)

```sh
curl -L -o llmchat https://github.com/overandor/llmchat/releases/latest/download/llmchat-linux-amd64
chmod +x llmchat && sudo mv llmchat /usr/local/bin/
llmchat
```

arm64: swap `llmchat-linux-amd64` → `llmchat-linux-arm64`.

## Providers

| `-provider` | endpoint | model | notes |
|---|---|---|---|
| `ollama` (default) | `http://localhost:11434` | auto-detected first local model | no key |
| `openai` | `https://api.openai.com` or any OpenAI-compatible base | e.g. `gpt-4o-mini` | `LLM_API_KEY` |
| `anthropic` | `https://api.anthropic.com` | `claude-sonnet-4-5-20250929` | `LLM_API_KEY` |

Any `/v1/chat/completions` service works via `-provider openai -endpoint <base>`: llm-proxy, Groq, Together, OpenRouter, vLLM, LM Studio.

## Config

Env or flags (flags win):

```
LLM_PROVIDER  LLM_ENDPOINT  LLM_API_KEY  LLM_MODEL  LLM_SYSTEM
-provider     -endpoint     -key         -model     -system   -temp
```

```sh
# Groq
LLM_PROVIDER=openai LLM_ENDPOINT=https://api.groq.com/openai/v1 \
  LLM_API_KEY=gsk_… LLM_MODEL=llama-3.3-70b-versatile llmchat

# Ollama on a remote box
llmchat -endpoint http://gpu-box:11434 -model qwen2.5:7b
```

## In the REPL

```
▸ type a message, Enter queues, empty line sends (multiline paste works)
/model qwen2.5:7b    switch model mid-session
/models              list local ollama models
/provider anthropic  hot-swap provider
/system you are terse
/clear               wipe context
/save notes.md       export transcript as markdown
/exit
```

Streams tokens, keeps full conversation context, refuses to poison history on API errors (failed turns are rolled back).

## Verify

```sh
llmchat -provider ollama   # auto-detects a local model
printf 'say exactly: ALIVE\n\n/exit\n' | llmchat   # scripted smoke test
```

Tested live against ollama/qwen2.5:0.5b — streaming + multi-turn recall verified.
