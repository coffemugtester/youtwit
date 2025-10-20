package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/spf13/cobra"
)

func GetSummary(command *cobra.Command, args []string){
	var summary string
	// TODO: pass the whole video url
	videoId, _ := command.Flags().GetString("videoId")
	useOllama, _ := command.Flags().GetBool("local")

	if _, after, found := strings.Cut(videoId, "="); found {
		videoId = after
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	transcript, err := GetTranscript(ctx, videoId)
	if err != nil {
		panic(fmt.Errorf("panic error: %s", err))
	}

	transcriptString := TranscriptToString(transcript)

	fmt.Println("~Tokens", len(transcriptString)/4)
	// TODO: wrap request functions into a client and structure a response
	if useOllama {
		resp, err := Generate(ctx, "", "qwen3:14b", fmt.Sprintf("Summarize the following transcript of a video into around 600 characters: %s", transcriptString))
	if err != nil {
			panic(fmt.Errorf("at ollama generation %s", err))
		}
		clean := StripThink(resp.Response)
		summary = makeSummary(videoId, clean)
	} else {

	openAiResponse, err := CallOpenAI(ctx, fmt.Sprintf("Summarize the following transcript of a video into around 600 characters: %s", transcriptString))
	if err != nil {
		panic(fmt.Errorf("at openAiResponse: %s", err))
	}

	openAiResponseText := openAiResponse.Output[0].Content[0].Text
	summary = makeSummary(videoId, openAiResponseText)
	}

	fmt.Printf("Local model: %v\n%s\n", useOllama, summary)
	clipboard.WriteAll(summary)
}


func makeSummary(videoId, response string) string {
	return fmt.Sprintf("https://www.youtube.com/watch?v=%s\nSummary: %s\n", videoId, response)
}
// TranscriptToString joins all `Text` fields from the transcript segments into a single string.
func TranscriptToString(segs TranscriptSegments) string {
	var sb strings.Builder
	for _, seg := range segs {
		if seg.Text != "" {
			sb.WriteString(seg.Text)
			sb.WriteString(" ") // add space between segments
		}
	}
	return strings.TrimSpace(sb.String())
}


func GetTranscript(ctx context.Context, videoID string) (TranscriptSegments, error) {
	var out TranscriptSegments

	if strings.Index(videoID, "-") == 0 {
		videoID = "\\" + videoID
	}

	// 1) Run the CLI (pipx/pip installed)
	cli := exec.CommandContext(ctx, "youtube_transcript_api", videoID, "--languages", "en", "es", "de", "pt")
	// --languages de en
	// --languages de en --exclude-generated
	// --languages de en --exclude-manually-created
	var cliStdout, cliStderr bytes.Buffer
	cli.Stdout = &cliStdout
	cli.Stderr = &cliStderr

	if err := cli.Run(); err != nil {
		return nil, fmt.Errorf("cli error: %w (stderr: %s)", err, strings.TrimSpace(cliStderr.String()))
	}
	rawRepr := cliStdout.Bytes()
	if len(rawRepr) == 0 {
		return nil, fmt.Errorf("empty output from youtube_transcript_api (stderr: %s)", strings.TrimSpace(cliStderr.String()))
	}

	// 2) Normalize Python repr → JSON using stdlib
	py := `import sys, ast, json; print(json.dumps(ast.literal_eval(sys.stdin.read())))`
	norm := exec.CommandContext(ctx, "python3", "-c", py)
	norm.Stdin = bytes.NewReader(rawRepr)
	var normStdout, normStderr bytes.Buffer
	norm.Stdout = &normStdout
	norm.Stderr = &normStderr

	if err := norm.Run(); err != nil {
		return nil, fmt.Errorf("repr→json error: %w (stderr: %s, raw: %s)",
			err, strings.TrimSpace(normStderr.String()), string(rawRepr))
	}
	jsonBytes := normStdout.Bytes()

	// 3) Decode JSON: try single list first, then list-of-lists
	if err := json.Unmarshal(jsonBytes, &out); err == nil {
		return out, nil
	}
	var batches []TranscriptSegments
	if err := json.Unmarshal(jsonBytes, &batches); err == nil {
		if len(batches) == 0 {
			return nil, fmt.Errorf("no transcripts in response")
		}
		return batches[0], nil
	}

	return nil, fmt.Errorf("unexpected JSON shape; sample: %.200s", string(jsonBytes))
}

const DefaultBaseURL = "http://localhost:11434"

// Generate calls /api/Generate once (non-streaming) and returns the parsed response.
// baseURL example: "http://localhost:11434"
func Generate(ctx context.Context, baseURL, model, prompt string) (*GenerateResponse, error) {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	reqBody := GenerateRequest{
		Model:  model,
		Prompt: prompt,
		Stream: false,
	}

	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := baseURL + "/api/generate"

	httpClient := &http.Client{Timeout: 2160 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, errors.New("ollama non-2xx: " + res.Status + " - " + string(body))
	}

	var out GenerateResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type GenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type GenerateResponse struct {
	Model              string    `json:"model"`
	CreatedAt          time.Time `json:"created_at"`
	Response           string    `json:"response"`
	Done               bool      `json:"done"`
	DoneReason         string    `json:"done_reason"`
	Context            []int     `json:"context"`
	TotalDuration      int64     `json:"total_duration"`
	LoadDuration       int64     `json:"load_duration"`
	PromptEvalCount    int       `json:"prompt_eval_count"`
	PromptEvalDuration int64     `json:"prompt_eval_duration"`
	EvalCount          int       `json:"eval_count"`
	EvalDuration       int64     `json:"eval_duration"`
}


var ThinkRe = regexp.MustCompile(`(?s)<think>[\s\S]*?</think>\s*`)

func StripThink(s string) string {
	return ThinkRe.ReplaceAllString(s, "")
}


func CallOpenAI(ctx context.Context, prompt string) (*OpenAIResponse, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("missing OPENAI_API_KEY environment variable")
	}

	// Build request body
	body := map[string]interface{}{
		"model": "gpt-4o-mini",
		"input": fmt.Sprintf("Provide a summary of the following transcript of a video in ~600 characters: %s", prompt),
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx,
		"POST", "https://api.openai.com/v1/responses", bytes.NewBuffer(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	// Do request
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second, // TCP connect
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second, // wait for headers
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     true,
	}

	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read response
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai API error: %s\n%s", resp.Status, data)
	}

	// Decode JSON
	var out OpenAIResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("unmarshal: %w\nraw: %s", err, data)
	}
	return &out, nil
}

