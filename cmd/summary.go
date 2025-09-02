package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/spf13/cobra"
)

// func init() {
// 	rootCmd.AddCommand(summarizeCmd)
// //
// // 	summarizeCmd.Flags().StringVarP(&videoId, "videoId", "v", "", "The ID of the video to be summarized")
// // 	_ = summarizeCmd.MarkFlagRequired("videoId")
// // }


func getSummary(cmd *cobra.Command, args []string){
	videoId, _ := cmd.Flags().GetString("videoId")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// TODO: handle No transcripts were found for any of the requested language codes: ['en']
	// For this video (wsDROfiAqxg) transcripts are available in the following languages:
	// TODO: handle video id's starting with `-`
	transcript, err := getTranscript(ctx, videoId)
	if err != nil {
		panic(fmt.Errorf("panic error: %s", err))
	}

	// fmt.Printf("Transcript: %+v\n", transcript)
	transcriptString := transcriptToString(transcript)

	// fmt.Printf("Transcript clean:\n%s\n", transcriptString)
	fmt.Println("~Tokens", len(transcriptString)/4)
	openAiResponse, err := callOpenAI(ctx, fmt.Sprintf("Summarize the following transcript of a video into around 600 characters: %s", transcriptString))
	if err != nil {
		panic(fmt.Errorf("at openAiResponse: %s", err))
	}

	openAiResponseText := openAiResponse.Output[0].Content[0].Text

	summary := fmt.Sprintf("https://www.youtube.com/watch?v=%s\nSummary: %s\n", videoId, openAiResponseText)

	fmt.Println(summary)
	clipboard.WriteAll(summary)

	// TODO: copy to clipboard
}


// transcriptToString joins all `Text` fields from the transcript segments into a single string.
func transcriptToString(segs TranscriptSegments) string {
	var sb strings.Builder
	for _, seg := range segs {
		if seg.Text != "" {
			sb.WriteString(seg.Text)
			sb.WriteString(" ") // add space between segments
		}
	}
	return strings.TrimSpace(sb.String())
}


func getTranscript(ctx context.Context, videoID string) (TranscriptSegments, error) {
	var out TranscriptSegments

	// 1) Run the CLI (pipx/pip installed)
	cli := exec.CommandContext(ctx, "youtube_transcript_api", videoID)
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

func callOpenAI(ctx context.Context, prompt string) (*OpenAIResponse, error) {
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


type TranscriptSegment struct {
    Duration float64 `json:"duration"`
    Start    float64 `json:"start"`
    Text     string  `json:"text,omitempty"`
}

type TranscriptSegments []TranscriptSegment

type OpenAIResponse struct {
	ID     string `json:"id"`
	Object string `json:"object"`
	// The "output" field of /responses can be more complex; here we keep it generic
	Output []OAImessage `json:"output"`
}

type OAImessage struct {
	ID      string       `json:"id"`
	Type    string       `json:"type"`
	Status  string       `json:"status"`
	Role    string       `json:"role"`
	Content []OAIcontent `json:"content"`
}

type OAIcontent struct {
	Type        string        `json:"type"`
	Text        string        `json:"text"`
	Annotations []interface{} `json:"annotations"`
	Logprobs    []interface{} `json:"logprobs"`
}

