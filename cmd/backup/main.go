package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"code.sirenko.ca/grocer/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	ctx := context.Background()

	// Get GCloud config
	gcsBucket := os.Getenv("GCS_BUCKET")
	gcsPrefix := os.Getenv("GCS_PREFIX")
	gcsCredsFile := os.Getenv("GCS_CREDENTIALS_FILE")

	if gcsBucket == "" {
		log.Fatal("GCS_BUCKET environment variable is required")
	}
	if gcsPrefix == "" {
		gcsPrefix = "snapshots/"
	}

	// Create GCloud storage
	gcs, err := store.NewGCloudStorage(ctx, gcsCredsFile, gcsBucket, gcsPrefix)
	if err != nil {
		log.Fatalf("Failed to create GCloud storage: %v", err)
	}
	defer gcs.Close()

	switch command {
	case "export":
		exportSnapshot(ctx, gcs)
	case "import":
		if len(os.Args) < 3 {
			log.Fatal("Usage: backup import <file>")
		}
		importSnapshot(ctx, gcs, os.Args[2])
	case "list":
		fmt.Println("Snapshot location: gs://" + gcsBucket + "/" + gcsPrefix + "snapshot.pb.gz")
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: backup <command> [args]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  export          Export snapshot from GCloud to local file")
	fmt.Println("  import <file>   Import local snapshot file to GCloud")
	fmt.Println("  list            List snapshot location")
}

func exportSnapshot(ctx context.Context, gcs *store.GCloudStorage) {
	data, err := gcs.Pull(ctx)
	if err != nil {
		log.Fatalf("Failed to pull snapshot: %v", err)
	}
	if data == nil {
		log.Fatal("No snapshot found in GCloud")
	}

	outputFile := "snapshot.pb.gz"
	if len(os.Args) > 2 {
		outputFile = os.Args[2]
	}

	if err := os.WriteFile(outputFile, data, 0644); err != nil {
		log.Fatalf("Failed to write file: %v", err)
	}

	fmt.Printf("Snapshot exported to %s (%d bytes)\n", outputFile, len(data))
}

func importSnapshot(ctx context.Context, gcs *store.GCloudStorage, inputFile string) {
	data, err := os.ReadFile(inputFile)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	if err := gcs.Push(ctx, data); err != nil {
		log.Fatalf("Failed to push snapshot: %v", err)
	}

	fmt.Printf("Snapshot imported from %s (%d bytes)\n", inputFile, len(data))
}
