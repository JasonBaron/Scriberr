package repository

import (
	"context"
	"testing"
	"time"

	"scriberr/internal/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newTestJobRepository(t *testing.T) (JobRepository, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.TranscriptionJob{}))

	return NewJobRepository(db), db
}

// ListWithParams used to build the ORDER BY clause by concatenating the
// caller-supplied sortBy and sortOrder values straight into the query.
// A sortBy value that isn't one of the allowed column names must not reach
// the query at all.
func TestListWithParams_RejectsUnknownSortColumn(t *testing.T) {
	repo, _ := newTestJobRepository(t)
	ctx := context.Background()

	// A payload that would be a syntax error (or worse, if it parsed) if it
	// ever reached the query as a raw ORDER BY fragment.
	maliciousSortBy := "id; DROP TABLE transcription_jobs; --"

	_, _, err := repo.ListWithParams(ctx, 0, 10, maliciousSortBy, "desc", "", nil)
	require.NoError(t, err, "an unrecognized sortBy should fall back to the default sort, not error or reach the query")
}

func TestListWithParams_AllowsKnownSortColumn(t *testing.T) {
	repo, db := newTestJobRepository(t)
	ctx := context.Background()

	title1 := "b-job"
	title2 := "a-job"
	require.NoError(t, db.Create(&models.TranscriptionJob{ID: "1", Title: &title1, AudioPath: "a.mp3"}).Error)
	require.NoError(t, db.Create(&models.TranscriptionJob{ID: "2", Title: &title2, AudioPath: "b.mp3"}).Error)

	jobs, count, err := repo.ListWithParams(ctx, 0, 10, "title", "asc", "", nil)
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
	require.Len(t, jobs, 2)
	require.Equal(t, "a-job", *jobs[0].Title)
	require.Equal(t, "b-job", *jobs[1].Title)
}

func TestSaveSuggestions(t *testing.T) {
	_, db := newTestJobRepository(t)
	require.NoError(t, db.AutoMigrate(&models.SummaryTemplate{}, &models.Summary{}))
	repo := NewSummaryRepository(db)
	ctx := context.Background()

	keep := "Weekly sync"
	transcript := "hello"
	require.NoError(t, db.Create(&models.TranscriptionJob{ID: "1", Title: &keep, AudioPath: "a.mp3", Transcript: &transcript}).Error)
	require.NoError(t, db.Create(&models.TranscriptionJob{ID: "2", AudioPath: "b.mp3"}).Error)
	require.NoError(t, db.Create(&models.TranscriptionJob{ID: "3", AudioPath: "c.mp3", Tags: models.StringList{"mine"}, TagsEdited: true}).Error)

	require.NoError(t, repo.SaveSuggestions(ctx, "1", models.JobSuggestion{Title: "2026-10-08 Budget review", Brief: "Q4 budget walkthrough.", Tags: models.StringList{"budget", "q4"}}, false))
	require.NoError(t, repo.SaveSuggestions(ctx, "2", models.JobSuggestion{Title: "2026-10-08 Hiring plan", Tags: models.StringList{"hiring"}}, true))
	require.NoError(t, repo.SaveSuggestions(ctx, "3", models.JobSuggestion{Title: "x", Tags: models.StringList{"generated"}}, false))

	var j1, j2, j3 models.TranscriptionJob
	require.NoError(t, db.First(&j1, "id = ?", "1").Error)
	require.NoError(t, db.First(&j2, "id = ?", "2").Error)
	require.NoError(t, db.First(&j3, "id = ?", "3").Error)

	require.Equal(t, "Weekly sync", *j1.Title, "a real title is kept")
	require.Equal(t, "2026-10-08 Budget review", *j1.SuggestedTitle)
	require.Equal(t, "Q4 budget walkthrough.", *j1.SummaryBrief)
	require.Equal(t, models.StringList{"budget", "q4"}, j1.Tags, "generated tags become the tags")
	require.Equal(t, "hello", *j1.Transcript, "other columns are untouched")

	require.Equal(t, "2026-10-08 Hiring plan", *j2.Title, "a placeholder title is replaced")
	require.Equal(t, models.StringList{"mine"}, j3.Tags, "edited tags are kept")
	require.Equal(t, models.StringList{"generated"}, j3.SuggestedTags)
}

func TestSetTagsAndTagCounts(t *testing.T) {
	_, db := newTestJobRepository(t)
	require.NoError(t, db.AutoMigrate(&models.SummaryTemplate{}))
	repo := NewSummaryRepository(db)
	ctx := context.Background()

	require.NoError(t, db.Create(&models.TranscriptionJob{ID: "1", AudioPath: "a.mp3", Tags: models.StringList{"family", "budget"}}).Error)
	require.NoError(t, db.Create(&models.TranscriptionJob{ID: "2", AudioPath: "b.mp3", Tags: models.StringList{"family"}}).Error)
	require.NoError(t, db.Create(&models.TranscriptionJob{ID: "3", AudioPath: "c.mp3"}).Error)

	require.NoError(t, repo.SetTags(ctx, "3", models.StringList{"family", "work"}))
	require.ErrorIs(t, repo.SetTags(ctx, "missing", models.StringList{"x"}), gorm.ErrRecordNotFound)

	var j3 models.TranscriptionJob
	require.NoError(t, db.First(&j3, "id = ?", "3").Error)
	require.True(t, j3.TagsEdited)

	counts, err := repo.TagCounts(ctx)
	require.NoError(t, err)
	require.Equal(t, []TagCount{{"family", 3}, {"budget", 1}, {"work", 1}}, counts)
}

func TestListSummaries(t *testing.T) {
	_, db := newTestJobRepository(t)
	require.NoError(t, db.AutoMigrate(&models.SummaryTemplate{}, &models.Summary{}))
	repo := NewSummaryRepository(db)
	ctx := context.Background()

	require.NoError(t, db.Create(&models.TranscriptionJob{ID: "1", AudioPath: "a.mp3"}).Error)
	require.NoError(t, repo.SaveSummary(ctx, &models.Summary{TranscriptionID: "1", Model: "m", Content: "first"}))
	require.NoError(t, repo.SaveSummary(ctx, &models.Summary{TranscriptionID: "1", Model: "m", Content: "second"}))

	list, err := repo.ListSummaries(ctx, "1")
	require.NoError(t, err)
	require.Len(t, list, 2)
}

func TestSetDefaultTemplate(t *testing.T) {
	_, db := newTestJobRepository(t)
	require.NoError(t, db.AutoMigrate(&models.SummaryTemplate{}))
	repo := NewSummaryRepository(db)
	ctx := context.Background()

	a := &models.SummaryTemplate{Name: "a", Model: "m", Prompt: "p", IsDefault: true}
	b := &models.SummaryTemplate{Name: "b", Model: "m", Prompt: "p"}
	require.NoError(t, repo.Create(ctx, a))
	require.NoError(t, repo.Create(ctx, b))

	require.NoError(t, repo.SetDefaultTemplate(ctx, b.ID))

	gotA, err := repo.FindByID(ctx, a.ID)
	require.NoError(t, err)
	gotB, err := repo.FindByID(ctx, b.ID)
	require.NoError(t, err)
	require.False(t, gotA.IsDefault)
	require.True(t, gotB.IsDefault)
}

func TestListWithParams_SearchesTags(t *testing.T) {
	repo, db := newTestJobRepository(t)
	ctx := context.Background()

	a, b := "Weekly sync", "Hiring plan"
	require.NoError(t, db.Create(&models.TranscriptionJob{ID: "1", Title: &a, AudioPath: "a.mp3", Tags: models.StringList{"budget", "q4"}}).Error)
	require.NoError(t, db.Create(&models.TranscriptionJob{ID: "2", Title: &b, AudioPath: "b.mp3"}).Error)

	jobs, count, err := repo.ListWithParams(ctx, 0, 10, "created_at", "desc", "budget", nil)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.Equal(t, "1", jobs[0].ID)
}

func TestSetRecordedAt(t *testing.T) {
	repo, db := newTestJobRepository(t)
	ctx := context.Background()
	require.NoError(t, db.Create(&models.TranscriptionJob{ID: "1", AudioPath: "a.m4a"}).Error)

	when := time.Date(2026, 9, 14, 17, 5, 22, 0, time.UTC)
	require.NoError(t, repo.SetRecordedAt(ctx, "1", when, "metadata"))

	var got models.TranscriptionJob
	require.NoError(t, db.First(&got, "id = ?", "1").Error)
	require.NotNil(t, got.RecordedAt)
	require.True(t, got.RecordedAt.Equal(when))
	require.Equal(t, "metadata", got.RecordedAtSource)
}

func TestFileHashLookups(t *testing.T) {
	repo, db := newTestJobRepository(t)
	ctx := context.Background()
	a := "First"
	require.NoError(t, db.Create(&models.TranscriptionJob{ID: "1", Title: &a, AudioPath: "a.m4a", FileHash: "abc"}).Error)
	require.NoError(t, db.Create(&models.TranscriptionJob{ID: "2", AudioPath: "b.m4a", FileHash: "abc"}).Error)
	require.NoError(t, db.Create(&models.TranscriptionJob{ID: "3", AudioPath: "c.m4a"}).Error)

	dups, err := repo.FindByFileHash(ctx, "abc", "2")
	require.NoError(t, err)
	require.Len(t, dups, 1)
	require.Equal(t, "1", dups[0].ID)
	require.Equal(t, "First", *dups[0].Title)

	missing, err := repo.ListMissingFileHash(ctx, 10)
	require.NoError(t, err)
	require.Len(t, missing, 1)
	require.Equal(t, "3", missing[0].ID)

	require.NoError(t, repo.SetFileHash(ctx, "3", "def", 42))
	missing, err = repo.ListMissingFileHash(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, missing)
}

func TestSummaryStatus(t *testing.T) {
	_, db := newTestJobRepository(t)
	require.NoError(t, db.AutoMigrate(&models.SummaryTemplate{}))
	repo := NewSummaryRepository(db)
	ctx := context.Background()
	require.NoError(t, db.Create(&models.TranscriptionJob{ID: "1", AudioPath: "a.mp3"}).Error)
	require.NoError(t, db.Create(&models.TranscriptionJob{ID: "2", AudioPath: "b.mp3"}).Error)

	require.NoError(t, repo.SetSummaryStatus(ctx, "1", models.SummaryRunning))
	var j models.TranscriptionJob
	require.NoError(t, db.First(&j, "id = ?", "1").Error)
	require.Equal(t, models.SummaryRunning, j.SummaryStatus)

	n, err := repo.ClearSummaryStatuses(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	require.NoError(t, db.First(&j, "id = ?", "1").Error)
	require.Empty(t, j.SummaryStatus)
}
