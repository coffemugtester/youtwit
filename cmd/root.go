package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use: "youtwit",
	Short: "Spend less time on YouTube without missing out on content",
	Long: "",
	Run: getSummary,
}

func Execute() {
	var videoId string

	rootCmd.Flags().StringVarP(&videoId, "videoId", "v", "", "The ID of the video to be summarized")
	_ = rootCmd.MarkFlagRequired("videoId")

	if err := rootCmd.Execute(); err != nil {
		panic(fmt.Sprintf("at cmd root: %s", err))
	}
}
