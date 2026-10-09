package repository

import (
	"context"
	"scriberr/internal/models"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

// transcriptionJobSortColumns are the columns ListWithParams allows sorting
// by. sortBy is a caller-supplied query param, so it has to be checked
// against a fixed set before use rather than passed straight into ORDER BY.
var transcriptionJobSortColumns = map[string]bool{
	"created_at": true,
	"updated_at": true,
	"title":      true,
	"status":     true,
	"audio_path": true,
}

// UserRepository handles user-specific database operations
type UserRepository interface {
	Repository[models.User]
	FindByUsername(ctx context.Context, username string) (*models.User, error)
	Count(ctx context.Context) (int64, error)
	CountWithAutoTranscription(ctx context.Context) (int64, error)
}

type userRepository struct {
	*BaseRepository[models.User]
}

func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepository{
		BaseRepository: NewBaseRepository[models.User](db),
	}
}

func (r *userRepository) FindByUsername(ctx context.Context, username string) (*models.User, error) {
	var user models.User
	err := r.db.WithContext(ctx).Where("username = ?", username).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.User{}).Count(&count).Error
	return count, err
}

func (r *userRepository) CountWithAutoTranscription(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.User{}).Where("auto_transcription_enabled = ?", true).Count(&count).Error
	return count, err
}

// JobRepository handles transcription job operations
type JobRepository interface {
	Repository[models.TranscriptionJob]
	FindWithAssociations(ctx context.Context, id string) (*models.TranscriptionJob, error)
	FindActiveTrackJobs(ctx context.Context, parentJobID string) ([]models.TranscriptionJob, error)
	FindLatestExecution(ctx context.Context, jobID string) (*models.TranscriptionJobExecution, error)
	FindLatestCompletedExecution(ctx context.Context, jobID string) (*models.TranscriptionJobExecution, error)
	ListWithParams(ctx context.Context, offset, limit int, sortBy, sortOrder, searchQuery string, updatedAfter *time.Time) ([]models.TranscriptionJob, int64, error)
	ListByUser(ctx context.Context, userID uint, offset, limit int) ([]models.TranscriptionJob, int64, error)
	UpdateTranscript(ctx context.Context, jobID string, transcript string) error
	CreateExecution(ctx context.Context, execution *models.TranscriptionJobExecution) error
	UpdateExecution(ctx context.Context, execution *models.TranscriptionJobExecution) error
	DeleteExecutionsByJobID(ctx context.Context, jobID string) error
	DeleteMultiTrackFilesByJobID(ctx context.Context, jobID string) error
	UpdateStatus(ctx context.Context, jobID string, status models.JobStatus) error
	UpdateError(ctx context.Context, jobID string, errorMsg string) error
	SetRecordedAt(ctx context.Context, jobID string, recordedAt time.Time, source string) error
	SetFileHash(ctx context.Context, jobID, hash string, size int64) error
	SetAudioPath(ctx context.Context, jobID, path string) error
	ListStorageInfo(ctx context.Context) ([]models.TranscriptionJob, error)
	FindByFileHash(ctx context.Context, hash, excludeID string) ([]models.TranscriptionJob, error)
	ListMissingFileHash(ctx context.Context, limit int) ([]models.TranscriptionJob, error)
	FindByStatus(ctx context.Context, status models.JobStatus) ([]models.TranscriptionJob, error)
	CountByStatus(ctx context.Context, status models.JobStatus) (int64, error)
	UpdateSummary(ctx context.Context, jobID string, summary string) error
}

type jobRepository struct {
	*BaseRepository[models.TranscriptionJob]
}

func NewJobRepository(db *gorm.DB) JobRepository {
	return &jobRepository{
		BaseRepository: NewBaseRepository[models.TranscriptionJob](db),
	}
}

func (r *jobRepository) FindWithAssociations(ctx context.Context, id string) (*models.TranscriptionJob, error) {
	var job models.TranscriptionJob
	err := r.db.WithContext(ctx).
		Preload("MultiTrackFiles").
		Where("id = ?", id).
		First(&job).Error
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *jobRepository) ListWithParams(ctx context.Context, offset, limit int, sortBy, sortOrder, searchQuery string, updatedAfter *time.Time) ([]models.TranscriptionJob, int64, error) {
	var jobs []models.TranscriptionJob
	var count int64

	db := r.db.WithContext(ctx).Model(&models.TranscriptionJob{})

	// Handle delta sync if updatedAfter provided
	if updatedAfter != nil {
		db = db.Unscoped().Where("updated_at > ?", *updatedAfter)
	}

	// Apply search filter
	if searchQuery != "" {
		search := "%" + searchQuery + "%"
		db = db.Where("title LIKE ? OR audio_path LIKE ? OR tags LIKE ? OR flags LIKE ?", search, search, search, search)
	}

	// Count total matching records
	if err := db.Count(&count).Error; err != nil {
		return nil, 0, err
	}

	// Apply sorting. sortBy and sortOrder come straight from query params, so
	// they're checked against a fixed set of values before being used to
	// build the ORDER BY clause, instead of being concatenated in directly.
	if transcriptionJobSortColumns[sortBy] {
		direction := "desc"
		if strings.EqualFold(sortOrder, "asc") {
			direction = "asc"
		}
		db = db.Order(sortBy + " " + direction)
	} else {
		// Default sort
		db = db.Order("created_at desc")
	}

	// Apply pagination
	err := db.Offset(offset).Limit(limit).Find(&jobs).Error
	if err != nil {
		return nil, 0, err
	}

	return jobs, count, nil
}

func (r *jobRepository) ListByUser(ctx context.Context, userID uint, offset, limit int) ([]models.TranscriptionJob, int64, error) {
	// Note: Currently TranscriptionJob doesn't have a UserID field in the provided model.
	// Assuming we might need to add it or this is a placeholder for future multi-user support.
	// For now, we'll just return all jobs as the current app seems single-user focused or
	// missing the link.
	// TODO: Add UserID to TranscriptionJob model if multi-user isolation is required.
	return r.List(ctx, offset, limit)
}

func (r *jobRepository) UpdateTranscript(ctx context.Context, jobID string, transcript string) error {
	return r.db.WithContext(ctx).Model(&models.TranscriptionJob{}).
		Where("id = ?", jobID).
		Update("transcript", transcript).Error
}

func (r *jobRepository) CreateExecution(ctx context.Context, execution *models.TranscriptionJobExecution) error {
	return r.db.WithContext(ctx).Create(execution).Error
}

func (r *jobRepository) UpdateExecution(ctx context.Context, execution *models.TranscriptionJobExecution) error {
	return r.db.WithContext(ctx).Save(execution).Error
}

func (r *jobRepository) DeleteExecutionsByJobID(ctx context.Context, jobID string) error {
	return r.db.WithContext(ctx).Where("transcription_job_id = ?", jobID).Delete(&models.TranscriptionJobExecution{}).Error
}

func (r *jobRepository) DeleteMultiTrackFilesByJobID(ctx context.Context, jobID string) error {
	return r.db.WithContext(ctx).Where("transcription_job_id = ?", jobID).Delete(&models.MultiTrackFile{}).Error
}

func (r *jobRepository) FindActiveTrackJobs(ctx context.Context, parentJobID string) ([]models.TranscriptionJob, error) {
	var jobs []models.TranscriptionJob
	err := r.db.WithContext(ctx).
		Where("id LIKE ? AND status IN (?)", "track_"+parentJobID+"_%", []string{"processing", "pending"}).
		Find(&jobs).Error
	return jobs, err
}

func (r *jobRepository) FindLatestCompletedExecution(ctx context.Context, jobID string) (*models.TranscriptionJobExecution, error) {
	var execution models.TranscriptionJobExecution
	err := r.db.WithContext(ctx).
		Where("transcription_job_id = ? AND status = ?", jobID, models.StatusCompleted).
		Order("created_at DESC").
		First(&execution).Error
	if err != nil {
		return nil, err
	}
	return &execution, nil
}

func (r *jobRepository) FindLatestExecution(ctx context.Context, jobID string) (*models.TranscriptionJobExecution, error) {
	var execution models.TranscriptionJobExecution
	err := r.db.WithContext(ctx).
		Where("transcription_job_id = ?", jobID).
		Order("created_at DESC").
		First(&execution).Error
	if err != nil {
		return nil, err
	}
	return &execution, nil
}

func (r *jobRepository) UpdateStatus(ctx context.Context, jobID string, status models.JobStatus) error {
	return r.db.WithContext(ctx).Model(&models.TranscriptionJob{}).Where("id = ?", jobID).Update("status", status).Error
}

func (r *jobRepository) UpdateError(ctx context.Context, jobID string, errorMsg string) error {
	return r.db.WithContext(ctx).Model(&models.TranscriptionJob{}).Where("id = ?", jobID).Update("error_message", errorMsg).Error
}

// SetAudioPath points a job at its audio file after it moves.
func (r *jobRepository) SetAudioPath(ctx context.Context, jobID, path string) error {
	return r.db.WithContext(ctx).Model(&models.TranscriptionJob{}).Where("id = ?", jobID).
		Update("audio_path", path).Error
}

// ListStorageInfo returns every job's ID, audio path, multi-track flag and
// creation time, without the heavy transcript columns.
func (r *jobRepository) ListStorageInfo(ctx context.Context) ([]models.TranscriptionJob, error) {
	var jobs []models.TranscriptionJob
	err := r.db.WithContext(ctx).Select("id", "audio_path", "is_multi_track", "created_at").Order("created_at ASC").Find(&jobs).Error
	return jobs, err
}

// SetFileHash stores the SHA-256 and size of a job's uploaded file.
func (r *jobRepository) SetFileHash(ctx context.Context, jobID, hash string, size int64) error {
	return r.db.WithContext(ctx).Model(&models.TranscriptionJob{}).Where("id = ?", jobID).
		Updates(map[string]interface{}{"file_hash": hash, "file_size": size}).Error
}

// FindByFileHash returns other jobs whose uploaded file has this hash,
// oldest first.
func (r *jobRepository) FindByFileHash(ctx context.Context, hash, excludeID string) ([]models.TranscriptionJob, error) {
	var jobs []models.TranscriptionJob
	if hash == "" {
		return jobs, nil
	}
	err := r.db.WithContext(ctx).Select("id", "title", "audio_path", "created_at").
		Where("file_hash = ? AND id <> ?", hash, excludeID).Order("created_at ASC").Find(&jobs).Error
	return jobs, err
}

// ListMissingFileHash returns jobs that have no file hash yet.
func (r *jobRepository) ListMissingFileHash(ctx context.Context, limit int) ([]models.TranscriptionJob, error) {
	var jobs []models.TranscriptionJob
	err := r.db.WithContext(ctx).Select("id", "audio_path").
		Where("(file_hash IS NULL OR file_hash = '') AND is_multi_track = ?", false).
		Order("created_at ASC").Limit(limit).Find(&jobs).Error
	return jobs, err
}

// SetRecordedAt stores when the audio was recorded and where that came from.
func (r *jobRepository) SetRecordedAt(ctx context.Context, jobID string, recordedAt time.Time, source string) error {
	return r.db.WithContext(ctx).Model(&models.TranscriptionJob{}).Where("id = ?", jobID).
		Updates(map[string]interface{}{"recorded_at": recordedAt.UTC(), "recorded_at_source": source}).Error
}

func (r *jobRepository) FindByStatus(ctx context.Context, status models.JobStatus) ([]models.TranscriptionJob, error) {
	var jobs []models.TranscriptionJob
	err := r.db.WithContext(ctx).Where("status = ?", status).Find(&jobs).Error
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

func (r *jobRepository) CountByStatus(ctx context.Context, status models.JobStatus) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.TranscriptionJob{}).Where("status = ?", status).Count(&count).Error
	return count, err
}

func (r *jobRepository) UpdateSummary(ctx context.Context, jobID string, summary string) error {
	return r.db.WithContext(ctx).Model(&models.TranscriptionJob{}).Where("id = ?", jobID).Update("summary", summary).Error
}

// APIKeyRepository handles API key operations
type APIKeyRepository interface {
	Repository[models.APIKey]
	FindByKey(ctx context.Context, key string) (*models.APIKey, error)
	ListActive(ctx context.Context) ([]models.APIKey, error)
	Revoke(ctx context.Context, id uint) error
}

type apiKeyRepository struct {
	*BaseRepository[models.APIKey]
}

func NewAPIKeyRepository(db *gorm.DB) APIKeyRepository {
	return &apiKeyRepository{
		BaseRepository: NewBaseRepository[models.APIKey](db),
	}
}

func (r *apiKeyRepository) FindByKey(ctx context.Context, key string) (*models.APIKey, error) {
	var apiKey models.APIKey
	err := r.db.WithContext(ctx).Where("key = ?", key).First(&apiKey).Error
	if err != nil {
		return nil, err
	}
	return &apiKey, nil
}

func (r *apiKeyRepository) ListActive(ctx context.Context) ([]models.APIKey, error) {
	var apiKeys []models.APIKey
	err := r.db.WithContext(ctx).Where("is_active = ?", true).Find(&apiKeys).Error
	if err != nil {
		return nil, err
	}
	return apiKeys, nil
}

func (r *apiKeyRepository) Revoke(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Model(&models.APIKey{}).Where("id = ?", id).Update("is_active", false).Error
}

// ProfileRepository handles transcription profile operations
type ProfileRepository interface {
	Repository[models.TranscriptionProfile]
	FindDefault(ctx context.Context) (*models.TranscriptionProfile, error)
	FindByName(ctx context.Context, name string) (*models.TranscriptionProfile, error)
}

type profileRepository struct {
	*BaseRepository[models.TranscriptionProfile]
}

func NewProfileRepository(db *gorm.DB) ProfileRepository {
	return &profileRepository{
		BaseRepository: NewBaseRepository[models.TranscriptionProfile](db),
	}
}

func (r *profileRepository) FindDefault(ctx context.Context) (*models.TranscriptionProfile, error) {
	var profile models.TranscriptionProfile
	err := r.db.WithContext(ctx).Where("is_default = ?", true).First(&profile).Error
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func (r *profileRepository) FindByName(ctx context.Context, name string) (*models.TranscriptionProfile, error) {
	var profile models.TranscriptionProfile
	err := r.db.WithContext(ctx).Where("name = ?", name).First(&profile).Error
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

// LLMConfigRepository handles LLM configuration operations
type LLMConfigRepository interface {
	Repository[models.LLMConfig]
	GetActive(ctx context.Context) (*models.LLMConfig, error)
}

type llmConfigRepository struct {
	*BaseRepository[models.LLMConfig]
}

func NewLLMConfigRepository(db *gorm.DB) LLMConfigRepository {
	return &llmConfigRepository{
		BaseRepository: NewBaseRepository[models.LLMConfig](db),
	}
}

func (r *llmConfigRepository) GetActive(ctx context.Context) (*models.LLMConfig, error) {
	var config models.LLMConfig
	err := r.db.WithContext(ctx).Where("is_active = ?", true).First(&config).Error
	if err != nil {
		return nil, err
	}
	return &config, nil
}

// SummaryRepository handles summary templates and settings
type SummaryRepository interface {
	Repository[models.SummaryTemplate]
	GetSettings(ctx context.Context) (*models.SummarySetting, error)
	SaveSettings(ctx context.Context, settings *models.SummarySetting) error
	SaveSummary(ctx context.Context, summary *models.Summary) error
	GetLatestSummary(ctx context.Context, transcriptionID string) (*models.Summary, error)
	DeleteByTranscriptionID(ctx context.Context, transcriptionID string) error
	ListSummaries(ctx context.Context, transcriptionID string) ([]models.Summary, error)
	SetDefaultTemplate(ctx context.Context, id string) error
	SaveSuggestions(ctx context.Context, jobID string, s models.JobSuggestion, applyTitle bool) error
	SetTags(ctx context.Context, jobID string, tags models.StringList) error
	TagCounts(ctx context.Context) ([]TagCount, error)
	SetSummaryStatus(ctx context.Context, jobID, status string) error
	HasTemplateSummary(ctx context.Context, jobID, templateID string) (bool, error)
	SummaryRunCandidates(ctx context.Context, f SummaryRunFilter) ([]string, error)
	ClearSummaryStatuses(ctx context.Context) (int64, error)
	MoveTagsToFlags(ctx context.Context, names []string) (int, error)
}

type summaryRepository struct {
	*BaseRepository[models.SummaryTemplate]
}

func NewSummaryRepository(db *gorm.DB) SummaryRepository {
	return &summaryRepository{
		BaseRepository: NewBaseRepository[models.SummaryTemplate](db),
	}
}

func (r *summaryRepository) GetSettings(ctx context.Context) (*models.SummarySetting, error) {
	var settings models.SummarySetting
	// Assuming singleton settings or per-user (but currently model might not have user_id)
	// If it's a singleton table:
	err := r.db.WithContext(ctx).First(&settings).Error
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

func (r *summaryRepository) SaveSettings(ctx context.Context, settings *models.SummarySetting) error {
	return r.db.WithContext(ctx).Save(settings).Error
}

func (r *summaryRepository) SaveSummary(ctx context.Context, summary *models.Summary) error {
	return r.db.WithContext(ctx).Create(summary).Error
}

func (r *summaryRepository) GetLatestSummary(ctx context.Context, transcriptionID string) (*models.Summary, error) {
	var summary models.Summary
	err := r.db.WithContext(ctx).Where("transcription_id = ?", transcriptionID).Order("created_at DESC").First(&summary).Error
	if err != nil {
		return nil, err
	}
	return &summary, nil
}

func (r *summaryRepository) DeleteByTranscriptionID(ctx context.Context, transcriptionID string) error {
	return r.db.WithContext(ctx).Where("transcription_id = ?", transcriptionID).Delete(&models.Summary{}).Error
}

// ChatRepository handles chat sessions and messages
type ChatRepository interface {
	Repository[models.ChatSession]
	GetSessionWithMessages(ctx context.Context, id string) (*models.ChatSession, error)
	GetSessionWithTranscription(ctx context.Context, id string) (*models.ChatSession, error)
	AddMessage(ctx context.Context, message *models.ChatMessage) error
	ListByJob(ctx context.Context, jobID string) ([]models.ChatSession, error)
	DeleteSession(ctx context.Context, id string) error
	GetMessages(ctx context.Context, sessionID string, limit int) ([]models.ChatMessage, error)
	DeleteByJobID(ctx context.Context, jobID string) error
	GetMessageCountsBySessionIDs(ctx context.Context, sessionIDs []string) (map[string]int64, error)
	GetLastMessagesBySessionIDs(ctx context.Context, sessionIDs []string) (map[string]*models.ChatMessage, error)
}

type chatRepository struct {
	*BaseRepository[models.ChatSession]
}

func NewChatRepository(db *gorm.DB) ChatRepository {
	return &chatRepository{
		BaseRepository: NewBaseRepository[models.ChatSession](db),
	}
}

func (r *chatRepository) GetSessionWithMessages(ctx context.Context, id string) (*models.ChatSession, error) {
	var session models.ChatSession
	err := r.db.WithContext(ctx).Preload("Messages").Where("id = ?", id).First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *chatRepository) GetSessionWithTranscription(ctx context.Context, id string) (*models.ChatSession, error) {
	var session models.ChatSession
	err := r.db.WithContext(ctx).Preload("Transcription").Where("id = ?", id).First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *chatRepository) AddMessage(ctx context.Context, message *models.ChatMessage) error {
	return r.db.WithContext(ctx).Create(message).Error
}

func (r *chatRepository) ListByJob(ctx context.Context, jobID string) ([]models.ChatSession, error) {
	var sessions []models.ChatSession
	err := r.db.WithContext(ctx).Where("transcription_id = ?", jobID).Order("created_at DESC").Find(&sessions).Error
	if err != nil {
		return nil, err
	}
	return sessions, nil
}

func (r *chatRepository) DeleteSession(ctx context.Context, id string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// Delete messages first
		if err := tx.Where("chat_session_id = ?", id).Delete(&models.ChatMessage{}).Error; err != nil {
			return err
		}
		// Delete session
		return tx.Delete(&models.ChatSession{}, "id = ?", id).Error
	})
}

func (r *chatRepository) DeleteByJobID(ctx context.Context, jobID string) error {
	// Find all sessions for this job
	var sessions []models.ChatSession
	if err := r.db.WithContext(ctx).Where("transcription_id = ?", jobID).Find(&sessions).Error; err != nil {
		return err
	}

	// Delete each session (which deletes messages)
	for _, session := range sessions {
		if err := r.DeleteSession(ctx, session.ID); err != nil {
			return err
		}
	}
	return nil
}

func (r *chatRepository) GetMessages(ctx context.Context, sessionID string, limit int) ([]models.ChatMessage, error) {
	var messages []models.ChatMessage
	query := r.db.WithContext(ctx).Where("chat_session_id = ?", sessionID).Order("created_at ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&messages).Error
	if err != nil {
		return nil, err
	}
	return messages, nil
}

func (r *chatRepository) GetMessageCountsBySessionIDs(ctx context.Context, sessionIDs []string) (map[string]int64, error) {
	if len(sessionIDs) == 0 {
		return make(map[string]int64), nil
	}

	type MessageCount struct {
		SessionID string `gorm:"column:session_id"`
		Count     int64  `gorm:"column:count"`
	}
	var counts []MessageCount

	err := r.db.WithContext(ctx).Model(&models.ChatMessage{}).
		Select("chat_session_id as session_id, COUNT(*) as count").
		Where("chat_session_id IN ?", sessionIDs).
		Group("chat_session_id").
		Scan(&counts).Error
	if err != nil {
		return nil, err
	}

	result := make(map[string]int64)
	for _, c := range counts {
		result[c.SessionID] = c.Count
	}
	return result, nil
}

func (r *chatRepository) GetLastMessagesBySessionIDs(ctx context.Context, sessionIDs []string) (map[string]*models.ChatMessage, error) {
	if len(sessionIDs) == 0 {
		return make(map[string]*models.ChatMessage), nil
	}

	var lastMessages []models.ChatMessage
	err := r.db.WithContext(ctx).Where(`id IN (
		SELECT id FROM chat_messages cm1
		WHERE cm1.chat_session_id IN ? 
		AND cm1.created_at = (
			SELECT MAX(cm2.created_at) 
			FROM chat_messages cm2 
			WHERE cm2.chat_session_id = cm1.chat_session_id
		)
	)`, sessionIDs).Find(&lastMessages).Error
	if err != nil {
		return nil, err
	}

	result := make(map[string]*models.ChatMessage)
	for i := range lastMessages {
		result[lastMessages[i].ChatSessionID] = &lastMessages[i]
	}
	return result, nil
}

// NoteRepository handles notes
type NoteRepository interface {
	Repository[models.Note]
	ListByJob(ctx context.Context, jobID string) ([]models.Note, error)
	DeleteByTranscriptionID(ctx context.Context, transcriptionID string) error
}

type noteRepository struct {
	*BaseRepository[models.Note]
}

func NewNoteRepository(db *gorm.DB) NoteRepository {
	return &noteRepository{
		BaseRepository: NewBaseRepository[models.Note](db),
	}
}

func (r *noteRepository) ListByJob(ctx context.Context, jobID string) ([]models.Note, error) {
	var notes []models.Note
	err := r.db.WithContext(ctx).Where("transcription_id = ?", jobID).Order("created_at DESC").Find(&notes).Error
	if err != nil {
		return nil, err
	}
	return notes, nil
}

func (r *noteRepository) DeleteByTranscriptionID(ctx context.Context, transcriptionID string) error {
	return r.db.WithContext(ctx).Where("transcription_id = ?", transcriptionID).Delete(&models.Note{}).Error
}

// SpeakerMappingRepository handles speaker mappings
type SpeakerMappingRepository interface {
	Repository[models.SpeakerMapping]
	ListByJob(ctx context.Context, jobID string) ([]models.SpeakerMapping, error)
	UpdateMappings(ctx context.Context, jobID string, mappings []models.SpeakerMapping) error
	DeleteByJobID(ctx context.Context, jobID string) error
}

type speakerMappingRepository struct {
	*BaseRepository[models.SpeakerMapping]
}

func NewSpeakerMappingRepository(db *gorm.DB) SpeakerMappingRepository {
	return &speakerMappingRepository{
		BaseRepository: NewBaseRepository[models.SpeakerMapping](db),
	}
}

func (r *speakerMappingRepository) ListByJob(ctx context.Context, jobID string) ([]models.SpeakerMapping, error) {
	var mappings []models.SpeakerMapping
	err := r.db.WithContext(ctx).Where("transcription_job_id = ?", jobID).Find(&mappings).Error
	if err != nil {
		return nil, err
	}
	return mappings, nil
}

func (r *speakerMappingRepository) DeleteByJobID(ctx context.Context, jobID string) error {
	return r.db.WithContext(ctx).Where("transcription_job_id = ?", jobID).Delete(&models.SpeakerMapping{}).Error
}

func (r *speakerMappingRepository) UpdateMappings(ctx context.Context, jobID string, mappings []models.SpeakerMapping) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// Delete existing mappings for this job
		if err := tx.Where("transcription_job_id = ?", jobID).Delete(&models.SpeakerMapping{}).Error; err != nil {
			return err
		}

		// Create new mappings
		if len(mappings) > 0 {
			if err := tx.Create(&mappings).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// RefreshTokenRepository handles refresh token operations
type RefreshTokenRepository interface {
	Create(ctx context.Context, token *models.RefreshToken) error
	FindByHash(ctx context.Context, hash string) (*models.RefreshToken, error)
	Revoke(ctx context.Context, id uint) error
	RevokeByHash(ctx context.Context, hash string) error
}

type refreshTokenRepository struct {
	db *gorm.DB
}

func NewRefreshTokenRepository(db *gorm.DB) RefreshTokenRepository {
	return &refreshTokenRepository{db: db}
}

func (r *refreshTokenRepository) Create(ctx context.Context, token *models.RefreshToken) error {
	return r.db.WithContext(ctx).Create(token).Error
}

func (r *refreshTokenRepository) FindByHash(ctx context.Context, hash string) (*models.RefreshToken, error) {
	var token models.RefreshToken
	err := r.db.WithContext(ctx).Where("hashed = ?", hash).First(&token).Error
	if err != nil {
		return nil, err
	}
	return &token, nil
}

func (r *refreshTokenRepository) Revoke(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Model(&models.RefreshToken{}).Where("id = ?", id).Update("revoked", true).Error
}

func (r *refreshTokenRepository) RevokeByHash(ctx context.Context, hash string) error {
	return r.db.WithContext(ctx).Model(&models.RefreshToken{}).Where("hashed = ?", hash).Update("revoked", true).Error
}

// ListSummaries returns every stored summary for a transcription, newest first.
func (r *summaryRepository) ListSummaries(ctx context.Context, transcriptionID string) ([]models.Summary, error) {
	var out []models.Summary
	err := r.db.WithContext(ctx).
		Select("id", "transcription_id", "template_id", "model", "content", "created_at", "updated_at").
		Where("transcription_id = ?", transcriptionID).
		Order("created_at DESC").
		Find(&out).Error
	return out, err
}

// SaveSuggestions stores the suggested title, brief and tags on a job.
// The tags also become the job's tags unless the user has edited them.
// With applyTitle the suggestion also replaces the job title. Only these
// columns are written, so a concurrent update to other fields is kept.
func (r *summaryRepository) SaveSuggestions(ctx context.Context, jobID string, s models.JobSuggestion, applyTitle bool) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updates := map[string]interface{}{
			"suggested_title": s.Title,
			"suggested_tags":  s.Tags,
			"flags":           s.Flags,
		}
		if s.Brief != "" {
			updates["summary_brief"] = s.Brief
		}
		if applyTitle {
			updates["title"] = s.Title
		}
		if err := tx.Model(&models.TranscriptionJob{}).Where("id = ?", jobID).Updates(updates).Error; err != nil {
			return err
		}
		if s.OverwriteEditedTags {
			return tx.Model(&models.TranscriptionJob{}).Where("id = ?", jobID).
				Updates(map[string]interface{}{"tags": s.Tags, "tags_edited": false}).Error
		}
		return tx.Model(&models.TranscriptionJob{}).
			Where("id = ? AND (tags_edited = ? OR tags_edited IS NULL)", jobID, false).
			Update("tags", s.Tags).Error
	})
}

// SetTags replaces a job's tags with ones the user chose. Later summaries
// no longer overwrite them.
func (r *summaryRepository) SetTags(ctx context.Context, jobID string, tags models.StringList) error {
	res := r.db.WithContext(ctx).Model(&models.TranscriptionJob{}).Where("id = ?", jobID).
		Updates(map[string]interface{}{"tags": tags, "tags_edited": true})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// SetSummaryStatus records the state of an automatic summary ("" when done).
func (r *summaryRepository) SetSummaryStatus(ctx context.Context, jobID, status string) error {
	return r.db.WithContext(ctx).Model(&models.TranscriptionJob{}).Where("id = ?", jobID).
		Update("summary_status", status).Error
}

// HasTemplateSummary reports whether a job already has a summary from the
// given template.
func (r *summaryRepository) HasTemplateSummary(ctx context.Context, jobID, templateID string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&models.Summary{}).
		Where("transcription_id = ? AND template_id = ?", jobID, templateID).Count(&n).Error
	return n > 0, err
}

// SummaryRunFilter selects recordings for a bulk summary run. Only completed
// jobs with a transcript are ever selected.
type SummaryRunFilter struct {
	JobIDs      []string
	Tag         string
	MissingOnly bool
	// HasSummary keeps recordings with at least one summary (retagging).
	HasSummary bool
	// SkipEdited drops recordings whose tags were edited by hand.
	SkipEdited bool
}

// SummaryRunCandidates returns the IDs of completed, transcribed jobs that
// match the filter, oldest first.
func (r *summaryRepository) SummaryRunCandidates(ctx context.Context, f SummaryRunFilter) ([]string, error) {
	q := r.db.WithContext(ctx).Model(&models.TranscriptionJob{}).
		Where("status = ? AND transcript IS NOT NULL AND transcript <> ''", models.StatusCompleted)
	if len(f.JobIDs) > 0 {
		q = q.Where("id IN ?", f.JobIDs)
	}
	if f.MissingOnly {
		q = q.Where("NOT EXISTS (SELECT 1 FROM summaries s WHERE s.transcription_id = transcription_jobs.id)")
	}
	if f.HasSummary {
		q = q.Where("EXISTS (SELECT 1 FROM summaries s WHERE s.transcription_id = transcription_jobs.id)")
	}
	if f.SkipEdited {
		q = q.Where("tags_edited = ? OR tags_edited IS NULL", false)
	}
	var rows []models.TranscriptionJob
	if err := q.Select("id", "tags").Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, j := range rows {
		if f.Tag != "" && !containsString(j.Tags, f.Tag) {
			continue
		}
		ids = append(ids, j.ID)
	}
	return ids, nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// MoveTagsToFlags moves the named tags (sensitive, pii) out of each
// recording's tags into its flags. Returns how many recordings changed.
func (r *summaryRepository) MoveTagsToFlags(ctx context.Context, names []string) (int, error) {
	move := map[string]bool{}
	for _, n := range names {
		move[n] = true
	}
	var jobs []models.TranscriptionJob
	if err := r.db.WithContext(ctx).Model(&models.TranscriptionJob{}).Select("id", "tags", "flags").
		Where("tags IS NOT NULL AND tags <> '' AND tags <> 'null'").Find(&jobs).Error; err != nil {
		return 0, err
	}
	changed := 0
	for _, j := range jobs {
		var keep models.StringList
		flags := append(models.StringList{}, j.Flags...)
		moved := false
		for _, t := range j.Tags {
			if !move[t] {
				keep = append(keep, t)
				continue
			}
			moved = true
			if !containsString(flags, t) {
				flags = append(flags, t)
			}
		}
		if !moved {
			continue
		}
		if keep == nil {
			keep = models.StringList{}
		}
		if err := r.db.WithContext(ctx).Model(&models.TranscriptionJob{}).Where("id = ?", j.ID).
			Updates(map[string]interface{}{"tags": keep, "flags": flags}).Error; err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

// ClearSummaryStatuses resets automatic summaries left pending by a restart.
func (r *summaryRepository) ClearSummaryStatuses(ctx context.Context) (int64, error) {
	res := r.db.WithContext(ctx).Model(&models.TranscriptionJob{}).
		Where("summary_status IS NOT NULL AND summary_status <> ''").Update("summary_status", "")
	return res.RowsAffected, res.Error
}

// TagCount is a tag and how many recordings use it.
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// TagCounts lists every tag in use, most used first.
func (r *summaryRepository) TagCounts(ctx context.Context) ([]TagCount, error) {
	var rows []models.StringList
	if err := r.db.WithContext(ctx).Model(&models.TranscriptionJob{}).
		Where("tags IS NOT NULL").Pluck("tags", &rows).Error; err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, tags := range rows {
		for _, t := range tags {
			counts[t]++
		}
	}
	out := make([]TagCount, 0, len(counts))
	for t, n := range counts {
		out = append(out, TagCount{Tag: t, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Tag < out[j].Tag
	})
	return out, nil
}

// SetDefaultTemplate marks one template as the default and clears the flag
// on every other template.
func (r *summaryRepository) SetDefaultTemplate(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.SummaryTemplate{}).Where("id <> ?", id).Update("is_default", false).Error; err != nil {
			return err
		}
		return tx.Model(&models.SummaryTemplate{}).Where("id = ?", id).Update("is_default", true).Error
	})
}
