package cmd

import (
	"youtwt/server"

	"github.com/spf13/cobra"
)

var startServer = &cobra.Command{
	Use: "ui",
	Short: "launch ui",
	Long: "",
	Run: server.Start,
}

func init() {
	var videoId string

	startServer.Flags().StringVarP(&videoId, "videoId", "v", "", "The ID of the video to be summarized")
	startServer.Flags().BoolP("local", "l", true, "Run local llm or not")

	rootCmd.AddCommand(startServer)
}
