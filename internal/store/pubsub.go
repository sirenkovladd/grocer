package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"cloud.google.com/go/pubsub"
	"google.golang.org/api/option"
)

// PubSubConfig configures a PubSubWatcher.
type PubSubConfig struct {
	// ProjectID is the GCP project that owns the topic. Required.
	ProjectID string

	// CredentialsFile is the path to a service account JSON file. If
	// empty, Application Default Credentials are used.
	CredentialsFile string

	// TopicID is the Pub/Sub topic that GCS publishes snapshot
	// change notifications to. Required. The topic must already
	// exist and the bucket must be configured to publish to it.
	TopicID string

	// SubscriptionID is the subscription name. If empty, defaults
	// to "grocer-snapshot-watcher-<hostname>" so each host gets
	// its own subscription and therefore receives every notification.
	// Sharing a subscription between hosts would give load-balancing
	// semantics (one notification goes to one host) — the opposite
	// of what we want.
	SubscriptionID string

	// SnapshotObject is the GCS object name to react to (e.g.,
	// "snapshots/snapshot.pb.gz"). Notifications for other objects
	// in the same bucket (e.g., photos) are ignored.
	SnapshotObject string
}

// PubSubWatcher subscribes to a Pub/Sub topic and calls
// store.ReloadSnapshot whenever a notification arrives for the
// configured snapshot object. This is how multiple grocer instances
// (e.g., local dev + remote server) stay in sync without polling.
//
// # Setup (one-time, run with gcloud)
//
// The topic and the bucket notification must be created manually
// before the watcher can run. Commands:
//
//	# 1. Create the topic.
//	gcloud pubsub topics create grocer-snapshot-changes \
//	  --project=$GCP_PROJECT_ID
//
//	# 2. Grant the GCS service account permission to publish to it.
//	#    (Replace the project number with your project's.)
//	gcloud pubsub topics add-iam-policy-binding grocer-snapshot-changes \
//	  --project=$GCP_PROJECT_ID \
//	  --member="serviceAccount:service-${PROJECT_NUMBER}@gs-project-iam.iam.gserviceaccount.com" \
//	  --role="roles/pubsub.publisher"
//
//	# 3. Wire the bucket to the topic. The --object-prefix limits
//	#    notifications to the snapshots/ folder, so photo uploads
//	#    don't wake the watcher.
//	gcloud storage buckets notifications create gs://$GCS_BUCKET \
//	  --project=$GCP_PROJECT_ID \
//	  --topic=grocer-snapshot-changes \
//	  --event-types=OBJECT_FINALIZE,OBJECT_DELETE \
//	  --object-prefix=$GCS_PREFIX
//
// Then set on the server: GCS_PUBSUB_TOPIC=grocer-snapshot-changes
// and GCP_PROJECT_ID=<project-id>.
//
// # Subscription model
//
// Each running host creates its own subscription named
// "grocer-snapshot-watcher-<hostname>". Both subscriptions receive
// every notification, so both hosts reload on every change. Sharing
// one subscription would give Pub/Sub's default load-balancing
// behavior, where each notification is delivered to exactly one
// host — the wrong semantics for state replication.
type PubSubWatcher struct {
	store        *Store
	subscription *pubsub.Subscription
	client       *pubsub.Client
	objectName   string
	subID        string
}

// NewPubSubWatcher creates a Pub/Sub subscriber wired to the given
// store. The subscription is created if it does not exist (the
// topic must already exist; see the setup commands in the type
// doc). Safe to call on every server start; the subscription
// creation is idempotent.
func NewPubSubWatcher(ctx context.Context, cfg PubSubConfig, store *Store) (*PubSubWatcher, error) {
	if cfg.TopicID == "" {
		return nil, errors.New("pubsub: TopicID is required")
	}
	if cfg.SnapshotObject == "" {
		return nil, errors.New("pubsub: SnapshotObject is required")
	}
	if store == nil {
		return nil, errors.New("pubsub: store is required")
	}

	// Resolve the GCP project ID. Prefer the explicit env var, fall
	// back to the project_id field in the credentials JSON. This
	// lets setups that already configure a service account (e.g.,
	// via GCS_CREDENTIALS_FILE) skip a second env var.
	projectID := cfg.ProjectID
	if projectID == "" && cfg.CredentialsFile != "" {
		resolved, err := projectIDFromCredentialsFile(cfg.CredentialsFile)
		if err != nil {
			return nil, fmt.Errorf("pubsub: resolve project ID from credentials file: %w", err)
		}
		projectID = resolved
	}
	if projectID == "" {
		return nil, errors.New("pubsub: ProjectID is required (set GCP_PROJECT_ID, or provide a credentials file with a project_id field)")
	}

	var opts []option.ClientOption
	if cfg.CredentialsFile != "" {
		opts = append(opts, option.WithCredentialsFile(cfg.CredentialsFile))
	}

	client, err := pubsub.NewClient(ctx, projectID, opts...)
	if err != nil {
		return nil, fmt.Errorf("pubsub.NewClient: %w", err)
	}

	subID := cfg.SubscriptionID
	if subID == "" {
		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "unknown"
		}
		subID = fmt.Sprintf("grocer-snapshot-watcher-%s", hostname)
	}

	topic := client.Topic(cfg.TopicID)
	sub := client.Subscription(subID)

	exists, err := sub.Exists(ctx)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("pubsub: check subscription %q: %w", subID, err)
	}
	if !exists {
		_, err := client.CreateSubscription(ctx, subID, pubsub.SubscriptionConfig{
			Topic:       topic,
			AckDeadline: 60 * time.Second,
		})
		if err != nil {
			client.Close()
			return nil, fmt.Errorf("pubsub: create subscription %q on topic %q: %w", subID, cfg.TopicID, err)
		}
		log.Printf("PubSubWatcher: created subscription %q on topic %q", subID, cfg.TopicID)
	} else {
		log.Printf("PubSubWatcher: using existing subscription %q on topic %q", subID, cfg.TopicID)
	}

	return &PubSubWatcher{
		store:        store,
		subscription: sub,
		client:       client,
		objectName:   cfg.SnapshotObject,
		subID:        subID,
	}, nil
}

// gcsNotification matches the JSON payload of a GCS Pub/Sub
// notification. See:
// https://cloud.google.com/storage/docs/pubsub-notifications#format
// Only the fields we filter on are decoded.
type gcsNotification struct {
	Name   string `json:"name"`
	Bucket string `json:"bucket"`
}

// Run starts processing messages. It blocks until ctx is cancelled
// or a fatal error occurs (e.g., the underlying client is closed).
// Designed to be called in its own goroutine.
//
// Each delivery filters on the object name, so unrelated events
// (e.g., photo uploads) are acked and ignored.
func (w *PubSubWatcher) Run(ctx context.Context) error {
	log.Printf("PubSubWatcher: started, watching object=%q (subscription=%q)", w.objectName, w.subID)
	defer log.Printf("PubSubWatcher: stopped")

	return w.subscription.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
		// Always Ack. If ReloadSnapshot fails transiently, the
		// next local save will republish and we'll catch up. If it
		// fails permanently (corrupt snapshot, etc.), Nack-and-retry
		// would loop forever — better to log and skip.
		defer msg.Ack()

		eventType, _ := msg.Attributes["eventType"]

		var notif gcsNotification
		if err := json.Unmarshal(msg.Data, &notif); err != nil {
			log.Printf("PubSubWatcher: parse notification failed: %v (skipping)", err)
			return
		}

		log.Printf("PubSubWatcher: received event=%s bucket=%s name=%s",
			eventType, notif.Bucket, notif.Name)

		// Filter: only react to our snapshot object. Photos live
		// in the same bucket and would otherwise trigger reloads.
		if notif.Name != w.objectName {
			return
		}

		switch eventType {
		case "OBJECT_FINALIZE":
			if err := w.store.ReloadSnapshot(ctx); err != nil {
				log.Printf("PubSubWatcher: reload failed: %v", err)
			}
		case "OBJECT_DELETE":
			// Snapshot was deleted in GCS. ReloadSnapshot already
			// handles this gracefully (preserves local state).
			log.Printf("PubSubWatcher: snapshot deleted in GCS")
			_ = w.store.ReloadSnapshot(ctx)
		default:
			// OBJECT_METADATA_UPDATE / OBJECT_ARCHIVE: not relevant.
		}
	})
}

// Close releases the underlying Pub/Sub client. Call this to
// cleanly shut down; the Run goroutine exits when its context is
// cancelled, not when Close is called.
func (w *PubSubWatcher) Close() error {
	return w.client.Close()
}
