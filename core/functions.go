package core

import (
	"bufio"
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

// Token limit configuration constants
const (
	MAX_SAFE_TOKENS  = 13950 // Maximum safe token count for transcripts
	PROMPT_OVERHEAD  = 25    // Estimated tokens used by prompt template
	RESPONSE_BUFFER  = 200   // Buffer for LLM response generation
	MAX_CHUNK_TOKENS = MAX_SAFE_TOKENS - PROMPT_OVERHEAD - RESPONSE_BUFFER // Effective maximum per chunk
)

// estimateTokens estimates the number of tokens in a given text
// Uses simple heuristic: 1 token ≈ 4 characters
func estimateTokens(text string) int {
	return len(text) / 4
}

// splitTranscript recursively splits a transcript into chunks that fit within token limits
// Returns a slice of transcript chunks
func splitTranscript(transcript string, maxTokens int) []string {
	tokens := estimateTokens(transcript)

	// Base case: transcript fits within limit
	if tokens <= maxTokens {
		return []string{transcript}
	}

	// Recursive case: split in half and process each half
	mid := len(transcript) / 2

	// Find a good split point near the middle (prefer splitting at spaces)
	splitPoint := mid
	searchRadius := len(transcript) / 20 // Search within 5% of midpoint

	for i := 0; i < searchRadius && mid+i < len(transcript); i++ {
		if transcript[mid+i] == ' ' || transcript[mid+i] == '\n' {
			splitPoint = mid + i
			break
		}
		if mid-i >= 0 && (transcript[mid-i] == ' ' || transcript[mid-i] == '\n') {
			splitPoint = mid - i
			break
		}
	}

	leftHalf := strings.TrimSpace(transcript[:splitPoint])
	rightHalf := strings.TrimSpace(transcript[splitPoint:])

	// Recursively split both halves
	leftChunks := splitTranscript(leftHalf, maxTokens)
	rightChunks := splitTranscript(rightHalf, maxTokens)

	// Combine results
	return append(leftChunks, rightChunks...)
}

// summarizeChunk summarizes a single transcript chunk using the specified LLM
func summarizeChunk(ctx context.Context, transcript, chunkLabel string, useOllama bool) (string, error) {
	var prompt string
	if chunkLabel != "" {
		prompt = fmt.Sprintf("Summarize the following transcript segment (%s) into around 600 characters: %s", chunkLabel, transcript)
	} else {
		prompt = fmt.Sprintf("Summarize the following transcript of a video into around 600 characters: %s", transcript)
	}

	if useOllama {
		resp, err := Generate(ctx, "", "qwen3:14b", prompt)
		if err != nil {
			return "", fmt.Errorf("ollama generation failed: %w", err)
		}
		return StripThink(resp.Response), nil
	} else {
		openAiResponse, err := CallOpenAI(ctx, transcript)
		if err != nil {
			return "", fmt.Errorf("openai request failed: %w", err)
		}
		return openAiResponse.Output[0].Content[0].Text, nil
	}
}

// mergeSummaries combines multiple partial summaries into one cohesive summary
func mergeSummaries(ctx context.Context, summaries []string, useOllama bool) (string, error) {
	if len(summaries) == 1 {
		return summaries[0], nil
	}

	// Join all summaries with clear separators
	var sb strings.Builder
	for i, summary := range summaries {
		sb.WriteString(fmt.Sprintf("Part %d: %s\n\n", i+1, summary))
	}

	prompt := fmt.Sprintf("The following are summaries of parts of a single video transcript. Combine them into one cohesive summary of around 600 characters:\n\n%s", sb.String())

	if useOllama {
		resp, err := Generate(ctx, "", "qwen3:14b", prompt)
		if err != nil {
			return "", fmt.Errorf("failed to merge summaries with ollama: %w", err)
		}
		return StripThink(resp.Response), nil
	} else {
		// For OpenAI, we need to pass the merged prompt directly
		openAiResponse, err := CallOpenAI(ctx, sb.String())
		if err != nil {
			return "", fmt.Errorf("failed to merge summaries with openai: %w", err)
		}
		return openAiResponse.Output[0].Content[0].Text, nil
	}
}

func GetSummary(command *cobra.Command, args []string){
	fmt.Print("Enter youtube URL: ")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	youtubeURL := strings.TrimSpace(scanner.Text())

	fmt.Println()

	useOllama, _ := command.Flags().GetBool("local")

	if _, after, found := strings.Cut(youtubeURL, "="); found {
		youtubeURL = after
	}

	// Initial context for fetching transcript
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	transcript, err := GetTranscript(ctx, youtubeURL)
	if err != nil {
		fmt.Printf("Error fetching transcript: %v\n", err)
		return
	}

	transcriptString := TranscriptToString(transcript)
	estimatedTokens := estimateTokens(transcriptString)

	fmt.Printf("~Tokens: %d\n", estimatedTokens)

	// Calculate dynamic timeout based on expected number of chunks
	timeout := 180 * time.Second
	if estimatedTokens > MAX_SAFE_TOKENS {
		numChunks := (estimatedTokens / MAX_CHUNK_TOKENS) + 1
		// 120s per chunk + 120s for merge operation
		timeout = time.Duration(120*numChunks+120) * time.Second
		fmt.Printf("Extended timeout to %v for %d chunks\n", timeout, numChunks)
	}

	// Replace context with appropriate timeout for summarization
	cancel() // Cancel old context
	ctx, cancel = context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var finalSummaryText string

	// Check if transcript needs to be split
	if estimatedTokens > MAX_SAFE_TOKENS {
		fmt.Printf("Transcript too long (%d tokens), splitting into chunks...\n", estimatedTokens)

		// Split transcript into manageable chunks
		chunks := splitTranscript(transcriptString, MAX_CHUNK_TOKENS)
		numChunks := len(chunks)

		fmt.Printf("Split into %d parts. Summarizing each part...\n", numChunks)

		// Summarize each chunk
		summaries := make([]string, 0, numChunks)
		for i, chunk := range chunks {
			chunkLabel := fmt.Sprintf("Part %d of %d", i+1, numChunks)
			fmt.Printf("Summarizing %s...\n", chunkLabel)

			chunkSummary, err := summarizeChunk(ctx, chunk, chunkLabel, useOllama)
			if err != nil {
				fmt.Printf("Error summarizing chunk %d: %v\n", i+1, err)
				return
			}
			summaries = append(summaries, chunkSummary)
		}

		// Merge all summaries into one cohesive summary
		fmt.Println("Merging summaries into final summary...")
		mergedSummary, err := mergeSummaries(ctx, summaries, useOllama)
		if err != nil {
			fmt.Printf("Error merging summaries: %v\n", err)
			return
		}
		finalSummaryText = mergedSummary

	} else {
		// Transcript fits within limits, summarize directly
		fmt.Println("Generating summary...")
		summaryText, err := summarizeChunk(ctx, transcriptString, "", useOllama)
		if err != nil {
			fmt.Printf("Error generating summary: %v\n", err)
			return
		}
		finalSummaryText = summaryText
	}

	summary := makeSummary(youtubeURL, finalSummaryText)
	fmt.Printf("\nLocal model: %v\n%s\n", useOllama, summary)

	if err := clipboard.WriteAll(summary); err != nil {
		fmt.Printf("Warning: Could not copy to clipboard: %v\n", err)
	} else {
		fmt.Println("(Summary copied to clipboard)")
	}
}


func makeSummary(youtubeURL, response string) string {
	return fmt.Sprintf("https://www.youtube.com/watch?v=%s\nSummary: %s\n", youtubeURL, response)
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


func GetTranscript(ctx context.Context, youtubeURL string) (TranscriptSegments, error) {
	var out TranscriptSegments

	if strings.Index(youtubeURL, "-") == 0 {
		youtubeURL = "\\" + youtubeURL
	}

	// 1) Run the CLI (pipx/pip installed)
	cli := exec.CommandContext(ctx, "youtube_transcript_api", youtubeURL, "--languages", "en", "es", "es-ES", "de", "pt")
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

