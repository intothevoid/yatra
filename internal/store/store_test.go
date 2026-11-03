package store

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/karan/travel-diary/internal/domain"
)

// setup creates a temporary store for testing.
func setup(t *testing.T) *Store {
	t.Helper()
	dir, err := os.MkdirTemp("", "store-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestTripImport(t *testing.T) {
	store := setup(t)
	ctx := context.Background()

	trip := domain.Trip{
		ID:        "t1",
		Name:      "Japan 2025",
		SourcePath: "/tmp/trips/japan",
		Status:    domain.StatusImported,
	}
	if err := store.Trips.Save(ctx, trip); err != nil {
		t.Fatalf("Save: %v", err)
	}

	fetched, err := store.Trips.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fetched.Name != "Japan 2025" {
		t.Errorf("Name = %q, want %q", fetched.Name, "Japan 2025")
	}
	if fetched.Status != domain.StatusImported {
		t.Errorf("Status = %q, want %q", fetched.Status, domain.StatusImported)
	}
}

func TestTripStatusTransition(t *testing.T) {
	store := setup(t)
	ctx := context.Background()

	trip := domain.Trip{
		ID:     "t1",
		Name:   "Test",
		Status: domain.StatusImported,
	}
	store.Trips.Save(ctx, trip)

	// Valid transition
	if err := store.Trips.UpdateStatus(ctx, "t1", domain.StatusAnalysing); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	fetched, _ := store.Trips.Get(ctx, "t1")
	if fetched.Status != domain.StatusAnalysing {
		t.Errorf("Status = %q, want %q", fetched.Status, domain.StatusAnalysing)
	}

	// Invalid transition
	if err := store.Trips.UpdateStatus(ctx, "t1", domain.StatusExportedDraft); err == nil {
		t.Error("UpdateStatus should fail for invalid transition")
	}
}

func TestSourceFileCRUD(t *testing.T) {
	store := setup(t)
	ctx := context.Background()

	// Create trip first
	trip := domain.Trip{ID: "t1", Name: "Test", Status: domain.StatusImported}
	store.Trips.Save(ctx, trip)

	// Save source file
	sf := domain.SourceFile{
		TripID:       "t1",
		RelativePath: "sample.jpg",
		MIME:         "image/jpeg",
		Bytes:        17705934,
		SHA256:       "abc123",
	}
	if err := store.Trips.SaveSourceFile(ctx, sf); err != nil {
		t.Fatalf("SaveSourceFile: %v", err)
	}

	// List source files
	files, err := store.Trips.ListSourceFiles(ctx, "t1")
	if err != nil {
		t.Fatalf("ListSourceFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("ListSourceFiles: got %d files, want 1", len(files))
	}
	if files[0].RelativePath != "sample.jpg" {
		t.Errorf("RelativePath = %q, want %q", files[0].RelativePath, "sample.jpg")
	}
}

func TestPhotoUpsert(t *testing.T) {
	store := setup(t)
	ctx := context.Background()

	photo := domain.Photo{
		ID:           "p1",
		SourceFileID: "t1_sample.jpg",
		Status:       domain.PhotoStatusRecommended,
		EXIF:         `{"make":"NIKON"}`,
	}
	if err := store.Photos.Upsert(ctx, photo); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	fetched, err := store.Photos.Get(ctx, "p1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fetched.Status != domain.PhotoStatusRecommended {
		t.Errorf("Status = %q, want %q", fetched.Status, domain.PhotoStatusRecommended)
	}
	if fetched.EXIF != `{"make":"NIKON"}` {
		t.Errorf("EXIF = %q, want %q", fetched.EXIF, `{"make":"NIKON"}`)
	}
}

func TestPhotoUpdateSelection(t *testing.T) {
	store := setup(t)
	ctx := context.Background()

	photo := domain.Photo{ID: "p1", SourceFileID: "t1_x", Status: domain.PhotoStatusUnknown}
	store.Photos.Upsert(ctx, photo)

	if err := store.Photos.UpdateSelection(ctx, "p1", domain.PhotoStatusRecommended); err != nil {
		t.Fatalf("UpdateSelection: %v", err)
	}

	fetched, _ := store.Photos.Get(ctx, "p1")
	if fetched.Status != domain.PhotoStatusRecommended {
		t.Errorf("Status = %q, want %q", fetched.Status, domain.PhotoStatusRecommended)
	}
}

func TestPhotoUpdateVLMResult(t *testing.T) {
	store := setup(t)
	ctx := context.Background()

	photo := domain.Photo{ID: "p1", SourceFileID: "t1_x", Status: domain.PhotoStatusUnknown}
	store.Photos.Upsert(ctx, photo)

	result := `{"schema_version":1,"photo_id":"p1","observed":{"description":"a bowl of ramen"}}`
	if err := store.Photos.UpdateVLMResult(ctx, "p1", result); err != nil {
		t.Fatalf("UpdateVLMResult: %v", err)
	}

	fetched, _ := store.Photos.Get(ctx, "p1")
	if fetched.VLMResult != result {
		t.Errorf("VLMResult = %q, want %q", fetched.VLMResult, result)
	}
}

func TestArticleRevision(t *testing.T) {
	store := setup(t)
	ctx := context.Background()

	// Create trip first (foreign key constraint)
	trip := domain.Trip{ID: "t1", Name: "Test", Status: domain.StatusImported}
	store.Trips.Save(ctx, trip)

	// Create article
	if err := store.Articles.Save(ctx, "t1", "Kyoto, Japan"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Save revision
	doc := domain.ArticleDocument{
		SchemaVersion: 1,
		ArticleID:     "art_t1",
		Title:         "Kyoto, Japan",
		Slug:          "kyoto-japan",
		Blocks: []domain.ArticleBlock{
			{ID: "b1", Type: domain.BlockHeading, Text: "Fushimi Inari", Level: 2},
			{ID: "b2", Type: domain.BlockParagraph, Text: "We reached the shrine early..."},
		},
	}
	docJSON, _ := json.Marshal(doc)
	rev := domain.ArticleRevision{
		ID:        "rev1",
		ArticleID: "art_t1",
		Author:    "system",
		Reason:    "initial generation",
		Document:  string(docJSON),
	}
	if err := store.Articles.SaveRevision(ctx, rev); err != nil {
		t.Fatalf("SaveRevision: %v", err)
	}

	// Get latest
	fetched, err := store.Articles.GetLatest(ctx, "art_t1")
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	if fetched.Title != "Kyoto, Japan" {
		t.Errorf("Title = %q, want %q", fetched.Title, "Kyoto, Japan")
	}
	if len(fetched.Blocks) != 2 {
		t.Errorf("Blocks = %d, want 2", len(fetched.Blocks))
	}
}

func TestJobClaimAndComplete(t *testing.T) {
	store := setup(t)
	ctx := context.Background()

	job := domain.Job{
		ID:           "j1",
		Type:         domain.JobTypeAnalysePhoto,
		TargetID:     "p1",
		IdempotencyKey: "key1",
	}
	if err := store.Jobs.Create(ctx, job); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Claim
	claimed, err := store.Jobs.Claim(ctx, "worker-1")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if claimed.ID != "j1" {
		t.Errorf("Claimed job ID = %q, want %q", claimed.ID, "j1")
	}
	if claimed.State != domain.JobStateRunning {
		t.Errorf("State = %q, want %q", claimed.State, domain.JobStateRunning)
	}

	// Complete
	if err := store.Jobs.Update(ctx, "j1", func(j *domain.Job) {
		j.State = domain.JobStateSucceeded
		j.Step = 1
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	fetched, _ := store.Jobs.Get(ctx, "j1")
	if fetched.State != domain.JobStateSucceeded {
		t.Errorf("State = %q, want %q", fetched.State, domain.JobStateSucceeded)
	}
}

func TestJobExpiredLeaseReclaim(t *testing.T) {
	store := setup(t)
	ctx := context.Background()

	job := domain.Job{
		ID:       "j1",
		Type:     domain.JobTypeAnalysePhoto,
		TargetID: "p1",
	}
	store.Jobs.Create(ctx, job)

	// Claim the job
	store.Jobs.Claim(ctx, "worker-1")

	// Manually set lease expiry to the past
	store.db.ExecContext(ctx, `
		UPDATE jobs SET lease_expiry = datetime('now', '-1 hour')
		WHERE id = 'j1'
	`)

	// Claim again — should reclaim the expired lease
	claimed, err := store.Jobs.Claim(ctx, "worker-2")
	if err != nil {
		t.Fatalf("Claim after expiry: %v", err)
	}
	if claimed.LeasedBy != "worker-2" {
		t.Errorf("LeasedBy = %q, want %q", claimed.LeasedBy, "worker-2")
	}
}

func TestJobCancellation(t *testing.T) {
	store := setup(t)
	ctx := context.Background()

	job := domain.Job{
		ID:       "j1",
		Type:     domain.JobTypeAnalysePhoto,
		TargetID: "p1",
	}
	store.Jobs.Create(ctx, job)

	if err := store.Jobs.Cancel(ctx, "j1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	fetched, err := store.Jobs.Get(ctx, "j1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !fetched.Cancelled {
		t.Error("Job should be cancelled")
	}
	if fetched.State != domain.JobStateCancelled {
		t.Errorf("State = %q, want %q", fetched.State, domain.JobStateCancelled)
	}

	// Cancelled jobs should not be claimable
	claimed, err := store.Jobs.Claim(ctx, "worker-1")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if claimed != nil {
		t.Error("Claim should return nil for cancelled jobs")
	}
}

func TestTripList(t *testing.T) {
	store := setup(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		trip := domain.Trip{
			ID:        "t" + string(rune('0'+i)),
			Name:      "Trip " + string(rune('A'+i)),
			Status:    domain.StatusImported,
			CreatedAt: time.Now().UTC().AddDate(0, 0, -i),
		}
		store.Trips.Save(ctx, trip)
	}

	trips, err := store.Trips.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(trips) != 3 {
		t.Fatalf("List: got %d trips, want 3", len(trips))
	}
}
