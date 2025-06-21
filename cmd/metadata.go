package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rtzll/tldw/internal"
)

// metadataCmd represents the metadata command
var metadataCmd = &cobra.Command{
	Use:   "metadata [URL]",
	Short: "Get metadata from YouTube video",
	Example: `  # Get metadata from YouTube video
  tldw metadata "https://www.youtube.com/watch?v=tAP1eZYEuKA"
  tldw metadata tAP1eZYEuKA

  # Save metadata to file
  tldw metadata tAP1eZYEuKA -o metadata.json

  # Format output as pretty JSON
  tldw metadata tAP1eZYEuKA --pretty`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app := internal.NewApp(config)
		youtubeURL, _ := internal.ParseArg(args[0])
		// Get metadata for the video
		metadata, err := app.Metadata(cmd.Context(), youtubeURL)
		if err != nil {
			return err
		}

		// Convert metadata to JSON
		var jsonData []byte
		pretty, _ := cmd.Flags().GetBool("pretty")
		if pretty {
			jsonData, err = json.MarshalIndent(metadata, "", "  ")
		} else {
			jsonData, err = json.Marshal(metadata)
		}
		if err != nil {
			return fmt.Errorf("error converting metadata to JSON: %w", err)
		}

		// Handle output flag
		outputFile, _ := cmd.Flags().GetString("output")
		if outputFile != "" {
			err := os.WriteFile(outputFile, jsonData, 0644)
			return err
		}

		fmt.Println(string(jsonData))

		return nil
	},
}

func init() {
	metadataCmd.Flags().StringP("output", "o", "", "Output file path (default: stdout)")
	metadataCmd.Flags().Bool("pretty", false, "Format output as pretty JSON")
	rootCmd.AddCommand(metadataCmd)
}
