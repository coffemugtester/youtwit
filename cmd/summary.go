package cmd

import (
	"context"
	"fmt"
	"time"

	"youtwt/cmd/internal"

	"github.com/atotto/clipboard"
	"github.com/spf13/cobra"
)

// func init() {
// 	rootCmd.AddCommand(summarizeCmd)
// 
//  	summarizeCmd.Flags().StringVarP(&videoId, "videoId", "v", "", "The ID of the video to be summarized")
//  	_ = summarizeCmd.MarkFlagRequired("videoId")
//  }


func getSummary(command *cobra.Command, args []string){
	var summary string
	videoId, _ := command.Flags().GetString("videoId")
	local, _ := command.Flags().GetBool("local")

	useOllama := local
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	transcript, err := cmd.GetTranscript(ctx, videoId)
	if err != nil {
		panic(fmt.Errorf("panic error: %s", err))
	}

	transcriptString := cmd.TranscriptToString(transcript)

	fmt.Println("~Tokens", len(transcriptString)/4)
	if useOllama {
		resp, err := cmd.Generate(ctx, "", "qwen3:14b", fmt.Sprintf("Summarize the following transcript of a video into around 600 characters: %s", transcriptString))
		fmt.Println(resp)
	if err != nil {
			panic(fmt.Errorf("at ollama generation %s", err))
		}
		clean := cmd.StripThink(resp.Response)
		summary = makeSummary(videoId, clean)
	} else {

	openAiResponse, err := cmd.CallOpenAI(ctx, fmt.Sprintf("Summarize the following transcript of a video into around 600 characters: %s", transcriptString))
	if err != nil {
		panic(fmt.Errorf("at openAiResponse: %s", err))
	}

	openAiResponseText := openAiResponse.Output[0].Content[0].Text
	summary = makeSummary(videoId, openAiResponseText)
	}

	fmt.Println("Local model:", local)
	fmt.Println(summary)
	clipboard.WriteAll(summary)
}

func makeSummary(videoId, response string) string {
	return fmt.Sprintf("https://www.youtube.com/watch?v=%s\nSummary: %s\n", videoId, response)
}
