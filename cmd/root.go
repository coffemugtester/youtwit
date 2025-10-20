package cmd

import (
	"fmt"
	"youtwt/core"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use: "youtwit",
	Short: "Spend less time on YouTube without missing out on content",
	Long: "",
	Run: core.GetSummary,
}

func Execute() {
	var videoId string

	rootCmd.Flags().StringVarP(&videoId, "videoId", "v", "", "The ID of the video to be summarized")
	rootCmd.Flags().BoolP("local", "l", true, "Run local llm or not")
	_ = rootCmd.MarkFlagRequired("videoId")

	if err := rootCmd.Execute(); err != nil {
		panic(fmt.Sprintf("at cmd root: %s", err))
	}
}
