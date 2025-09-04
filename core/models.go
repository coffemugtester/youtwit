package core

type TranscriptSegment struct {
    Duration float64 `json:"duration"`
    Start    float64 `json:"start"`
    Text     string  `json:"text,omitempty"`
}

type TranscriptSegments []TranscriptSegment

type OpenAIResponse struct {
	ID     string `json:"id"`
	Object string `json:"object"`
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

