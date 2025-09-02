# Video Transcript Summarizer

> A simple tool to summarize video transcripts and extract the main idea — helping you decide whether a video is worth watching.

---

## 🎯 Purpose

This tool allows you to quickly **get the main idea** of a video by summarizing its transcript. Instead of watching the entire video, you can read a concise summary to determine if it's relevant or useful for your interests.

---

## 🔍 How It Works

1. **Input**: Provide a video transcript (text file or pasted text).
2. **Processing**: The tool analyzes the transcript to identify key themes, topics, and the overall message.
3. **Output**: A clear, concise summary that highlights the main idea and important points.

---

## 📌 Features

- **Fast summarization** of long transcripts.
- **Focus on the main idea** — no fluff or unnecessary details.
- **Easy to use** — just paste the youtube video id

---

## 📦 Prerequisites (if applicable)

```bash
pipx install video-transcript-api
```
Add a key for OPENAI_API_KEY
`go build -o ...` and put the binary on PATH

---

## Usage:


`youtwit -v=<videoId>`

---

## 🧰 Example

**Input Transcript (simplified):**

> `youtwit -v=<videoId>`

**Output Summary:**

>  "In this video, we explore the history of artificial intelligence. Starting from the 1950s, researchers have made significant progress. Today, AI is used in various industries, from healthcare to finance. However, there are still ethical concerns and challenges to overcome."

