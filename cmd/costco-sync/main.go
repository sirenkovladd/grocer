// Command costco-sync pulls Costco warehouse receipts through the user's
// logged-in Brave tab and commits them directly as receipts — with no
// server running. It reads and writes the same GCS snapshot the server
// uses; a running server picks the new snapshot up via its Pub/Sub
// watcher, a stopped one on next boot.
//
// Single-operator tool: do not run it while another write is in flight.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"code.sirenko.ca/grocer/internal/costcosync"
	"code.sirenko.ca/grocer/internal/llm"
	"code.sirenko.ca/grocer/internal/receipt"
	"code.sirenko.ca/grocer/internal/store"
)

func main() {
	owner := flag.String("owner", "", "Grocer username to tag receipts with (default: the single user)")
	since := flag.String("since", "1/01/2025", "List start date (M/D/YYYY)")
	until := flag.String("until", "", "List end date (M/D/YYYY, default today)")
	timezone := flag.String("timezone", "", "IANA timezone for receipt dates (default system local)")
	bravePort := flag.Int("brave-port", 9222, "Brave remote-debugging port")
	dryRun := flag.Bool("dry-run", false, "Fetch and parse only; no store writes, no snapshot push")
	flag.Parse()

	end := *until
	if end == "" {
		end = time.Now().Format("1/2/2006")
	}
	loc := time.Local
	if *timezone != "" {
		var err error
		loc, err = time.LoadLocation(*timezone)
		if err != nil {
			log.Fatalf("bad --timezone %q: %v", *timezone, err)
		}
	}

	ctx := context.Background()

	s, err := store.NewStore()
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	bucket := os.Getenv("GCS_BUCKET")
	if bucket == "" {
		log.Fatalf("GCS_BUCKET is not set (run via mise so .env loads, e.g. mise run sync_costco)")
	}
	prefix := os.Getenv("GCS_PREFIX")
	if prefix == "" {
		prefix = "snapshots/"
	}
	gcs, err := store.NewGCloudStorage(ctx, os.Getenv("GCS_CREDENTIALS_FILE"), bucket, prefix)
	if err != nil {
		log.Fatalf("snapshot storage: %v", err)
	}
	defer gcs.Close()
	s.SetSnapshotStorage(gcs)
	if err := s.LoadSnapshot(ctx); err != nil {
		log.Fatalf("load snapshot: %v", err)
	}

	var provider llm.Provider
	switch os.Getenv("LLM_PROVIDER") {
	case "kimi":
		provider = llm.NewKimiProvider(os.Getenv("LLM_API_KEY"), os.Getenv("LLM_MODEL"))
	case "qwen":
		provider = llm.NewQwenProvider(os.Getenv("LLM_API_KEY"), os.Getenv("LLM_MODEL"))
	case "minimax":
		provider = llm.NewMinimaxProvider(os.Getenv("LLM_API_KEY"), os.Getenv("LLM_MODEL"))
	default:
		log.Fatalf("unknown LLM_PROVIDER %q", os.Getenv("LLM_PROVIDER"))
	}
	parser := receipt.NewParser(s, provider)

	ownerID, err := costcosync.ResolveOwner(s, *owner)
	if err != nil {
		log.Fatalf("owner: %v", err)
	}

	cdp, err := costcosync.DialCDP(fmt.Sprintf("http://localhost:%d", *bravePort))
	if err != nil {
		log.Fatalf("brave (port %d): %v — is Brave open with remote debugging?", *bravePort, err)
	}
	defer cdp.Close()

	sum, err := costcosync.Run(ctx, costcosync.Cfg{
		CDP: cdp, Store: s, Parser: parser,
		OwnerID: ownerID, Since: *since, Until: end,
		Timezone: loc, DryRun: *dryRun,
	})
	if err != nil {
		log.Fatalf("sync: %v", err)
	}
	for _, f := range sum.Failed {
		fmt.Printf("failed %s: %s\n", f.Barcode, f.Err)
	}
	dry := ""
	if *dryRun {
		dry = " (dry run, nothing written)"
	}
	fmt.Printf("created %d, skipped %d, failed %d%s\n", sum.Created, sum.Skipped, len(sum.Failed), dry)
	if len(sum.Failed) > 0 {
		os.Exit(1)
	}
	if !*dryRun && costcosync.ShouldPush(sum.Created) {
		if err := s.SaveSnapshot(ctx); err != nil {
			log.Fatalf("save snapshot: %v", err)
		}
		fmt.Println("snapshot saved")
	}
}
