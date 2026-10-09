package tests

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scriberr/internal/api"
	"scriberr/internal/models"
	"scriberr/internal/processing"
	"scriberr/internal/queue"
	"scriberr/internal/recordings"
	"scriberr/internal/repository"
	"scriberr/internal/service"
	"scriberr/internal/sse"
	"scriberr/internal/transcription"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type APIHandlerTestSuite struct {
	suite.Suite
	helper             *TestHelper
	router             *gin.Engine
	handler            *api.Handler
	taskQueue          *queue.TaskQueue
	unifiedProcessor   *transcription.UnifiedJobProcessor
	quickTranscription *transcription.QuickTranscriptionService
	mockOpenAI         *httptest.Server
}

func (suite *APIHandlerTestSuite) SetupSuite() {
	suite.helper = NewTestHelper(suite.T(), "api_handlers_test.db")
	suite.mockOpenAI = NewMockOpenAIServer()

	// Initialize repositories
	jobRepo := repository.NewJobRepository(suite.helper.DB)
	userRepo := repository.NewUserRepository(suite.helper.DB)
	apiKeyRepo := repository.NewAPIKeyRepository(suite.helper.DB)
	profileRepo := repository.NewProfileRepository(suite.helper.DB)
	llmConfigRepo := repository.NewLLMConfigRepository(suite.helper.DB)
	summaryRepo := repository.NewSummaryRepository(suite.helper.DB)
	chatRepo := repository.NewChatRepository(suite.helper.DB)
	noteRepo := repository.NewNoteRepository(suite.helper.DB)
	speakerMappingRepo := repository.NewSpeakerMappingRepository(suite.helper.DB)
	refreshTokenRepo := repository.NewRefreshTokenRepository(suite.helper.DB)

	// Initialize services
	userService := service.NewUserService(userRepo, suite.helper.AuthService)
	fileService := service.NewFileService()

	// Initialize services
	suite.unifiedProcessor = transcription.NewUnifiedJobProcessor(jobRepo, suite.helper.Config.TempDir, suite.helper.Config.TranscriptsDir)
	var err error
	suite.quickTranscription, err = transcription.NewQuickTranscriptionService(suite.helper.Config, suite.unifiedProcessor, jobRepo)
	assert.NoError(suite.T(), err)

	suite.taskQueue = queue.NewTaskQueue(1, suite.unifiedProcessor, jobRepo)

	broadcaster := sse.NewBroadcaster()

	multiTrackProcessor := processing.NewMultiTrackProcessor(suite.helper.DB, jobRepo)

	suite.handler = api.NewHandler(
		suite.helper.Config,
		suite.helper.AuthService,
		userService,
		fileService,
		jobRepo,
		apiKeyRepo,
		profileRepo,
		userRepo,
		llmConfigRepo,
		summaryRepo,
		chatRepo,
		noteRepo,
		speakerMappingRepo,
		refreshTokenRepo,
		suite.taskQueue,
		suite.unifiedProcessor,
		suite.quickTranscription,
		multiTrackProcessor,
		broadcaster,
	)

	// Set up router
	suite.router = api.SetupRoutes(suite.handler, suite.helper.AuthService)
}

func (suite *APIHandlerTestSuite) TearDownSuite() {
	if suite.mockOpenAI != nil {
		suite.mockOpenAI.Close()
	}
	suite.helper.Cleanup()
}

func (suite *APIHandlerTestSuite) SetupTest() {
	suite.helper.ResetDB(suite.T())

	// Create LLM config pointing to mock server
	llmConfig := &models.LLMConfig{
		Provider:      "openai",
		OpenAIBaseURL: &suite.mockOpenAI.URL,
		APIKey:        stringPtr("test-api-key"),
		IsActive:      true,
	}
	err := suite.helper.DB.Create(llmConfig).Error
	assert.NoError(suite.T(), err)
}

// Helper method to make authenticated requests
func (suite *APIHandlerTestSuite) makeAuthenticatedRequest(method, path string, body interface{}, useJWT bool) *httptest.ResponseRecorder {
	var req *http.Request
	var err error

	if body != nil {
		switch v := body.(type) {
		case string:
			req, err = http.NewRequest(method, path, strings.NewReader(v))
		case []byte:
			req, err = http.NewRequest(method, path, bytes.NewBuffer(v))
		case *bytes.Buffer:
			req, err = http.NewRequest(method, path, v)
		default:
			jsonBody, _ := json.Marshal(v)
			req, err = http.NewRequest(method, path, bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
		}
	} else {
		req, err = http.NewRequest(method, path, nil)
	}

	assert.NoError(suite.T(), err)

	// Add authentication
	if useJWT {
		req.Header.Set("Authorization", "Bearer "+suite.helper.TestToken)
	} else {
		req.Header.Set("X-API-Key", suite.helper.TestAPIKey)
	}

	w := httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)
	return w
}

// Test health check endpoint
func (suite *APIHandlerTestSuite) TestHealthCheck() {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	suite.router.ServeHTTP(w, req)

	assert.Equal(suite.T(), 200, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "healthy", response["status"])
}

// Test user registration
func (suite *APIHandlerTestSuite) TestRegisterUser() {
	registerData := map[string]string{
		"username": "newuser123",
		"password": "newpassword123",
	}

	jsonData, _ := json.Marshal(registerData)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/auth/register", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	suite.router.ServeHTTP(w, req)

	// Should return 400 because registration might be disabled or user already exists
	assert.True(suite.T(), w.Code == 200 || w.Code == 400 || w.Code == 409)
}

// Test user login
func (suite *APIHandlerTestSuite) TestLoginUser() {
	loginData := map[string]string{
		"username": suite.helper.TestUser.Username,
		"password": "testpassword123",
	}

	jsonData, _ := json.Marshal(loginData)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/auth/login", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	suite.router.ServeHTTP(w, req)

	assert.Equal(suite.T(), 200, w.Code)

	var response api.LoginResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(suite.T(), err)
	assert.NotEmpty(suite.T(), response.Token)
	assert.Equal(suite.T(), suite.helper.TestUser.Username, response.User.Username)
}

// Test getting registration status
func (suite *APIHandlerTestSuite) TestGetRegistrationStatus() {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/auth/registration-status", nil)
	suite.router.ServeHTTP(w, req)

	assert.Equal(suite.T(), 200, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(suite.T(), err)
	assert.Contains(suite.T(), response, "registration_enabled")
}

// Test API key management
func (suite *APIHandlerTestSuite) TestAPIKeyManagement() {
	// List API keys (JWT required)
	w := suite.makeAuthenticatedRequest("GET", "/api/v1/api-keys/", nil, true)
	assert.Equal(suite.T(), 200, w.Code)

	var wrappedResponse struct {
		APIKeys []struct {
			ID          uint   `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			KeyPreview  string `json:"key_preview"`
			IsActive    bool   `json:"is_active"`
			CreatedAt   string `json:"created_at"`
			UpdatedAt   string `json:"updated_at"`
			LastUsed    string `json:"last_used"`
		} `json:"api_keys"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &wrappedResponse)
	assert.NoError(suite.T(), err)

	// Should contain at least our test API key (check by key preview)
	found := false
	testKeyPreview := suite.helper.TestAPIKey[:8] + "..."
	for _, key := range wrappedResponse.APIKeys {
		if key.KeyPreview == testKeyPreview {
			found = true
			break
		}
	}
	assert.True(suite.T(), found)

	// Create new API key (JWT required)
	createData := map[string]string{
		"name":        "Test Created Key",
		"description": "Key created during testing",
	}

	w = suite.makeAuthenticatedRequest("POST", "/api/v1/api-keys/", createData, true)
	assert.Equal(suite.T(), 200, w.Code)

	var createResponse models.APIKey
	err = json.Unmarshal(w.Body.Bytes(), &createResponse)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "Test Created Key", createResponse.Name)
	assert.NotEmpty(suite.T(), createResponse.Key)

	// Delete the created API key
	w = suite.makeAuthenticatedRequest("DELETE", fmt.Sprintf("/api/v1/api-keys/%d", createResponse.ID), nil, true)
	assert.Equal(suite.T(), 200, w.Code)
}

// Test transcription job listing
func (suite *APIHandlerTestSuite) TestListTranscriptionJobs() {
	// Create a test job first
	testJob := suite.helper.CreateTestTranscriptionJob(suite.T(), "Test Job for Listing")

	w := suite.makeAuthenticatedRequest("GET", "/api/v1/transcription/list", nil, false)
	assert.Equal(suite.T(), 200, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(suite.T(), err)

	assert.Contains(suite.T(), response, "jobs")
	assert.Contains(suite.T(), response, "pagination")

	jobs := response["jobs"].([]interface{})
	assert.GreaterOrEqual(suite.T(), len(jobs), 1)

	// Check if our test job is in the list
	foundJob := false
	for _, job := range jobs {
		jobMap := job.(map[string]interface{})
		if jobMap["id"] == testJob.ID {
			foundJob = true
			break
		}
	}
	assert.True(suite.T(), foundJob)
}

// Test transcription job listing with delta sync
func (suite *APIHandlerTestSuite) TestListTranscriptionJobsDeltaSync() {
	// 1. Create a job
	job1 := suite.helper.CreateTestTranscriptionJob(suite.T(), "Job 1 (Active)")
	time.Sleep(10 * time.Millisecond) // Ensure unique timestamp

	// 2. Create another job
	job2 := suite.helper.CreateTestTranscriptionJob(suite.T(), "Job 2 (To Be Deleted)")
	time.Sleep(10 * time.Millisecond)

	// Capture time before deletion (but after creation)
	syncTime := time.Now().Add(-5 * time.Second) // Set sync time to slightly before now to pick up these jobs if they updated?
	// Actually, we want to test:
	// - created job is returned
	// - deleted job is returned if updated_after < deletion_time

	// Let's delete job2
	w := suite.makeAuthenticatedRequest("DELETE", fmt.Sprintf("/api/v1/transcription/%s", job2.ID), nil, false)
	assert.Equal(suite.T(), 200, w.Code)

	// Case A: Normal List (No param) -> Should return job1, NOT job2
	w = suite.makeAuthenticatedRequest("GET", "/api/v1/transcription/list", nil, false)
	assert.Equal(suite.T(), 200, w.Code)
	var responseStandard map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &responseStandard)
	jobsStd := responseStandard["jobs"].([]interface{})

	foundJob1 := false
	foundJob2 := false
	for _, j := range jobsStd {
		jm := j.(map[string]interface{})
		if jm["id"] == job1.ID {
			foundJob1 = true
		}
		if jm["id"] == job2.ID {
			foundJob2 = true
		}
	}
	assert.True(suite.T(), foundJob1, "Active job should be found in standard list")
	assert.False(suite.T(), foundJob2, "Deleted job should NOT be found in standard list")

	// Case B: Delta Sync (updated_after)
	// We want to see both jobs because both were updated (created or deleted) recently.
	updatedAfter := syncTime.Format(time.RFC3339)
	w = suite.makeAuthenticatedRequest("GET", fmt.Sprintf("/api/v1/transcription/list?updated_after=%s", updatedAfter), nil, false)
	assert.Equal(suite.T(), 200, w.Code)

	var responseDelta map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &responseDelta)
	jobsDelta := responseDelta["jobs"].([]interface{})

	foundJob1 = false
	foundJob2 = false
	var job2Data map[string]interface{}

	for _, j := range jobsDelta {
		jm := j.(map[string]interface{})
		if jm["id"] == job1.ID {
			foundJob1 = true
		}
		if jm["id"] == job2.ID {
			foundJob2 = true
			job2Data = jm
		}
	}
	assert.True(suite.T(), foundJob1, "Active job should be found in delta sync")
	assert.True(suite.T(), foundJob2, "Deleted job SHOULD be found in delta sync")

	// Verify deleted_at is set for job2
	if job2Data != nil {
		_, hasDeletedAt := job2Data["deleted_at"]
		// deleted_at might be nil or string
		assert.True(suite.T(), hasDeletedAt, "deleted_at field should be present")
		assert.NotNil(suite.T(), job2Data["deleted_at"], "deleted_at should not be nil for deleted job")
	}
}

// Test getting transcription job by ID
func (suite *APIHandlerTestSuite) TestGetTranscriptionJobByID() {
	testJob := suite.helper.CreateTestTranscriptionJob(suite.T(), "Test Job by ID")

	w := suite.makeAuthenticatedRequest("GET", fmt.Sprintf("/api/v1/transcription/%s", testJob.ID), nil, false)
	assert.Equal(suite.T(), 200, w.Code)

	var response models.TranscriptionJob
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), testJob.ID, response.ID)
	assert.Equal(suite.T(), *testJob.Title, *response.Title)
}

// Test getting job status
func (suite *APIHandlerTestSuite) TestGetJobStatus() {
	testJob := suite.helper.CreateTestTranscriptionJob(suite.T(), "Test Job Status")

	w := suite.makeAuthenticatedRequest("GET", fmt.Sprintf("/api/v1/transcription/%s/status", testJob.ID), nil, false)
	assert.Equal(suite.T(), 200, w.Code)

	var response models.TranscriptionJob
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), testJob.ID, response.ID)
	assert.Equal(suite.T(), models.StatusPending, response.Status)
}

// Test updating transcription title
func (suite *APIHandlerTestSuite) TestUpdateTranscriptionTitle() {
	testJob := suite.helper.CreateTestTranscriptionJob(suite.T(), "Original Title")

	updateData := map[string]string{
		"title": "Updated Title",
	}

	w := suite.makeAuthenticatedRequest("PUT", fmt.Sprintf("/api/v1/transcription/%s/title", testJob.ID), updateData, false)
	assert.Equal(suite.T(), 200, w.Code)

	// Verify the title was updated
	w = suite.makeAuthenticatedRequest("GET", fmt.Sprintf("/api/v1/transcription/%s", testJob.ID), nil, false)
	assert.Equal(suite.T(), 200, w.Code)

	var response models.TranscriptionJob
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "Updated Title", *response.Title)
}

// Test listing every stored summary for a job, with template names
func (suite *APIHandlerTestSuite) TestListSummariesForTranscription() {
	testJob := suite.helper.CreateTestTranscriptionJob(suite.T(), "Summaries")
	tpl := suite.helper.CreateTestSummaryTemplate(suite.T(), "Brief")
	ctx := context.Background()
	repo := repository.NewSummaryRepository(suite.helper.DB)
	assert.NoError(suite.T(), repo.SaveSummary(ctx, &models.Summary{TranscriptionID: testJob.ID, TemplateID: &tpl.ID, Model: "m", Content: "one"}))
	assert.NoError(suite.T(), repo.SaveSummary(ctx, &models.Summary{TranscriptionID: testJob.ID, Model: "m", Content: "two"}))

	w := suite.makeAuthenticatedRequest("GET", fmt.Sprintf("/api/v1/transcription/%s/summaries", testJob.ID), nil, false)
	assert.Equal(suite.T(), 200, w.Code)

	var items []api.SummaryListItem
	assert.NoError(suite.T(), json.Unmarshal(w.Body.Bytes(), &items))
	assert.Len(suite.T(), items, 2)
	names := map[string]string{}
	for _, it := range items {
		names[it.Content] = it.TemplateName
	}
	assert.Equal(suite.T(), "Brief", names["one"])
	assert.Equal(suite.T(), "", names["two"])
}

// Test editing tags and listing the tags in use
func (suite *APIHandlerTestSuite) TestUpdateTagsAndListTags() {
	testJob := suite.helper.CreateTestTranscriptionJob(suite.T(), "Tagged")

	w := suite.makeAuthenticatedRequest("PUT", fmt.Sprintf("/api/v1/transcription/%s/tags", testJob.ID), map[string]interface{}{
		"tags": []string{" Family ", "family", "#Q4_Planning"},
	}, false)
	assert.Equal(suite.T(), 200, w.Code)

	w = suite.makeAuthenticatedRequest("GET", fmt.Sprintf("/api/v1/transcription/%s", testJob.ID), nil, false)
	var job models.TranscriptionJob
	assert.NoError(suite.T(), json.Unmarshal(w.Body.Bytes(), &job))
	assert.Equal(suite.T(), models.StringList{"family", "q4 planning"}, job.Tags)
	assert.True(suite.T(), job.TagsEdited)

	w = suite.makeAuthenticatedRequest("GET", "/api/v1/tags", nil, false)
	assert.Equal(suite.T(), 200, w.Code)
	var counts []repository.TagCount
	assert.NoError(suite.T(), json.Unmarshal(w.Body.Bytes(), &counts))
	assert.Len(suite.T(), counts, 2)

	w = suite.makeAuthenticatedRequest("PUT", "/api/v1/transcription/missing/tags", map[string]interface{}{"tags": []string{"x"}}, false)
	assert.Equal(suite.T(), 404, w.Code)
}

// Test that summary settings keep fields that are not sent
func (suite *APIHandlerTestSuite) TestSummarySettingsAutoSummarize() {
	w := suite.makeAuthenticatedRequest("POST", "/api/v1/summaries/settings", map[string]interface{}{"default_model": "m1"}, false)
	assert.Equal(suite.T(), 200, w.Code)
	w = suite.makeAuthenticatedRequest("POST", "/api/v1/summaries/settings", map[string]interface{}{"auto_summarize": true}, false)
	assert.Equal(suite.T(), 200, w.Code)

	w = suite.makeAuthenticatedRequest("GET", "/api/v1/summaries/settings", nil, false)
	var resp api.SummarySettingsResponse
	assert.NoError(suite.T(), json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(suite.T(), "m1", resp.DefaultModel)
	assert.True(suite.T(), resp.AutoSummarize)
}

// Test deleting transcription job
func (suite *APIHandlerTestSuite) TestDeleteTranscriptionJob() {
	testJob := suite.helper.CreateTestTranscriptionJob(suite.T(), "Job to Delete")

	w := suite.makeAuthenticatedRequest("DELETE", fmt.Sprintf("/api/v1/transcription/%s", testJob.ID), nil, false)
	assert.Equal(suite.T(), 200, w.Code)

	// Verify the job was deleted
	w = suite.makeAuthenticatedRequest("GET", fmt.Sprintf("/api/v1/transcription/%s", testJob.ID), nil, false)
	assert.Equal(suite.T(), 404, w.Code)
}

// Test getting supported models
func (suite *APIHandlerTestSuite) TestGetSupportedModels() {
	w := suite.makeAuthenticatedRequest("GET", "/api/v1/transcription/models", nil, false)
	assert.Equal(suite.T(), 200, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(suite.T(), err)

	assert.Contains(suite.T(), response, "models")
	assert.Contains(suite.T(), response, "languages")

	// Models is now a map (model_id -> capabilities), languages is still an array
	// In test environment, these may be empty since no adapters are registered
	models := response["models"].(map[string]interface{})
	languages := response["languages"].([]interface{})

	// Just verify they have the correct types (may be empty in test environment)
	assert.NotNil(suite.T(), models)
	assert.NotNil(suite.T(), languages)
}

// Test profile management
func (suite *APIHandlerTestSuite) TestProfileManagement() {
	// List profiles
	w := suite.makeAuthenticatedRequest("GET", "/api/v1/profiles/", nil, false)
	assert.Equal(suite.T(), 200, w.Code)

	// Create profile
	profileData := map[string]interface{}{
		"name":        "Test Profile",
		"description": "Test profile description",
		"parameters": map[string]interface{}{
			"model":      "base",
			"batch_size": 16,
			"device":     "auto",
		},
	}

	w = suite.makeAuthenticatedRequest("POST", "/api/v1/profiles/", profileData, false)
	assert.Equal(suite.T(), 200, w.Code)

	var createResponse models.TranscriptionProfile
	err := json.Unmarshal(w.Body.Bytes(), &createResponse)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "Test Profile", createResponse.Name)

	// Get profile
	w = suite.makeAuthenticatedRequest("GET", fmt.Sprintf("/api/v1/profiles/%s", createResponse.ID), nil, false)
	assert.Equal(suite.T(), 200, w.Code)

	// Update profile
	updateData := map[string]interface{}{
		"name":        "Updated Profile",
		"description": "Updated description",
	}

	w = suite.makeAuthenticatedRequest("PUT", fmt.Sprintf("/api/v1/profiles/%s", createResponse.ID), updateData, false)
	assert.Equal(suite.T(), 200, w.Code)

	// Delete profile
	w = suite.makeAuthenticatedRequest("DELETE", fmt.Sprintf("/api/v1/profiles/%s", createResponse.ID), nil, false)
	assert.Equal(suite.T(), 200, w.Code)
}

// Test notes management
func (suite *APIHandlerTestSuite) TestNotesManagement() {
	// Create a transcription job first
	testJob := suite.helper.CreateTestTranscriptionJob(suite.T(), "Job for Notes")

	// Create note
	noteData := map[string]interface{}{
		"start_word_index": 0,
		"end_word_index":   5,
		"start_time":       0.0,
		"end_time":         2.5,
		"quote":            "Test quote text",
		"content":          "Test note content",
	}

	w := suite.makeAuthenticatedRequest("POST", fmt.Sprintf("/api/v1/transcription/%s/notes", testJob.ID), noteData, false)
	assert.Equal(suite.T(), 200, w.Code)

	var createResponse models.Note
	err := json.Unmarshal(w.Body.Bytes(), &createResponse)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "Test note content", createResponse.Content)

	// List notes for transcription
	w = suite.makeAuthenticatedRequest("GET", fmt.Sprintf("/api/v1/transcription/%s/notes", testJob.ID), nil, false)
	assert.Equal(suite.T(), 200, w.Code)

	var listResponse []models.Note
	err = json.Unmarshal(w.Body.Bytes(), &listResponse)
	assert.NoError(suite.T(), err)
	assert.GreaterOrEqual(suite.T(), len(listResponse), 1)

	// Update note
	updateData := map[string]string{
		"content": "Updated note content",
	}

	w = suite.makeAuthenticatedRequest("PUT", fmt.Sprintf("/api/v1/notes/%s", createResponse.ID), updateData, false)
	assert.Equal(suite.T(), 200, w.Code)

	// Get updated note
	w = suite.makeAuthenticatedRequest("GET", fmt.Sprintf("/api/v1/notes/%s", createResponse.ID), nil, false)
	assert.Equal(suite.T(), 200, w.Code)

	var updatedNote models.Note
	err = json.Unmarshal(w.Body.Bytes(), &updatedNote)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "Updated note content", updatedNote.Content)

	// Delete note
	w = suite.makeAuthenticatedRequest("DELETE", fmt.Sprintf("/api/v1/notes/%s", createResponse.ID), nil, false)
	assert.Equal(suite.T(), 200, w.Code)
}

// Test queue stats
func (suite *APIHandlerTestSuite) TestGetQueueStats() {
	w := suite.makeAuthenticatedRequest("GET", "/api/v1/admin/queue/stats", nil, false)
	assert.Equal(suite.T(), 200, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(suite.T(), err)

	assert.Contains(suite.T(), response, "queue_size")
	assert.Contains(suite.T(), response, "current_workers")
	assert.Contains(suite.T(), response, "pending_jobs")
	assert.Contains(suite.T(), response, "processing_jobs")
	assert.Contains(suite.T(), response, "completed_jobs")
	assert.Contains(suite.T(), response, "failed_jobs")
}

// Test multipart file upload (transcription submit)
func (suite *APIHandlerTestSuite) TestTranscriptionSubmit() {
	// Create a dummy audio file
	tmpFile, err := os.CreateTemp("", "test_audio_*.mp3")
	assert.NoError(suite.T(), err)
	defer os.Remove(tmpFile.Name())

	tmpFile.WriteString("dummy audio data for API handler testing")
	tmpFile.Close()

	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add audio file
	file, err := os.Open(tmpFile.Name())
	assert.NoError(suite.T(), err)
	defer file.Close()

	part, err := writer.CreateFormFile("audio", "test.mp3")
	assert.NoError(suite.T(), err)
	io.Copy(part, file)

	// Add form fields
	writer.WriteField("title", "API Handler Test Audio")
	writer.WriteField("model", "base")
	writer.WriteField("diarization", "false")

	writer.Close()

	req, _ := http.NewRequest("POST", "/api/v1/transcription/submit", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-API-Key", suite.helper.TestAPIKey)

	w := httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)

	assert.Equal(suite.T(), 200, w.Code)

	var response models.TranscriptionJob
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(suite.T(), err)
	assert.NotEmpty(suite.T(), response.ID)
	assert.Equal(suite.T(), "API Handler Test Audio", *response.Title)
	assert.Equal(suite.T(), models.StatusPending, response.Status)
}

// uploadBytes posts data to /transcription/upload and returns the job.
func (suite *APIHandlerTestSuite) uploadBytes(name string, data []byte) models.TranscriptionJob {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("audio", name)
	assert.NoError(suite.T(), err)
	_, _ = part.Write(data)
	_ = writer.WriteField("title", name)
	writer.Close()

	req, _ := http.NewRequest("POST", "/api/v1/transcription/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-API-Key", suite.helper.TestAPIKey)
	w := httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)
	assert.Equal(suite.T(), 200, w.Code, w.Body.String())

	var job models.TranscriptionJob
	assert.NoError(suite.T(), json.Unmarshal(w.Body.Bytes(), &job))
	return job
}

// Test that uploads are hashed and repeat uploads are flagged
func (suite *APIHandlerTestSuite) TestUploadDuplicateDetection() {
	data := []byte("same audio bytes for duplicate detection")
	first := suite.uploadBytes("memo.m4a", data)
	assert.Len(suite.T(), first.FileHash, 64)
	assert.Equal(suite.T(), int64(len(data)), first.FileSize)
	assert.Equal(suite.T(), "memo.m4a", first.OriginalFilename)
	assert.Empty(suite.T(), first.Duplicates)

	second := suite.uploadBytes("memo copy.m4a", data)
	assert.Equal(suite.T(), first.FileHash, second.FileHash)
	if assert.Len(suite.T(), second.Duplicates, 1) {
		assert.Equal(suite.T(), first.ID, second.Duplicates[0].ID)
	}

	other := suite.uploadBytes("other.m4a", []byte("different bytes"))
	assert.Empty(suite.T(), other.Duplicates)

	w := suite.makeAuthenticatedRequest("GET", fmt.Sprintf("/api/v1/transcription/%s/file-info", first.ID), nil, false)
	assert.Equal(suite.T(), 200, w.Code)
	var info api.FileInfoResponse
	assert.NoError(suite.T(), json.Unmarshal(w.Body.Bytes(), &info))
	assert.Equal(suite.T(), first.FileHash, info.SHA256)
	assert.Equal(suite.T(), "memo.m4a", info.OriginalFilename)
	assert.True(suite.T(), info.FileExists)
	if assert.Len(suite.T(), info.Duplicates, 1) {
		assert.Equal(suite.T(), second.ID, info.Duplicates[0].ID)
	}

	w = suite.makeAuthenticatedRequest("GET", "/api/v1/transcription/missing/file-info", nil, false)
	assert.Equal(suite.T(), 404, w.Code)
}

// Test moving existing recordings into per-recording folders, and that new
// uploads, logs, summaries and deletes use them
func (suite *APIHandlerTestSuite) TestRecordingFolders() {
	t := suite.T()
	cfg := suite.helper.Config
	root, _ := filepath.Abs(filepath.Join(filepath.Dir(cfg.UploadDir), "recordings_test"))
	store := recordings.Store{Root: root}
	assert.NoError(t, store.CheckSameDevice(cfg.UploadDir))
	suite.handler.SetRecordings(store)
	defer func() {
		suite.handler.SetRecordings(recordings.Store{})
		os.RemoveAll(root)
	}()
	ctx := context.Background()

	// A job in the old layout: uploads/<id>.wav and transcripts/<id>/transcription.log
	job := suite.helper.CreateTestTranscriptionJob(t, "Legacy recording")
	legacyAudio := filepath.Join(cfg.UploadDir, job.ID+".wav")
	assert.NoError(t, os.MkdirAll(cfg.UploadDir, 0o755))
	assert.NoError(t, os.WriteFile(legacyAudio, []byte("audio"), 0o644))
	legacyDir := filepath.Join(cfg.TranscriptsDir, job.ID)
	assert.NoError(t, os.MkdirAll(legacyDir, 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(legacyDir, "transcription.log"), []byte("JOB LOG"), 0o644))
	assert.NoError(t, suite.helper.DB.Model(&models.TranscriptionJob{}).Where("id = ?", job.ID).Update("audio_path", legacyAudio).Error)
	repo := repository.NewSummaryRepository(suite.helper.DB)
	assert.NoError(t, repo.SaveSummary(ctx, &models.Summary{TranscriptionID: job.ID, Model: "m", Content: "## Overview\nText"}))

	suite.handler.MigrateRecordings(ctx)

	folder := filepath.Join(root, store.FolderName(job.ID, time.Now()))
	newAudio := filepath.Join(folder, "audio.wav")
	_, err := os.Stat(newAudio)
	assert.NoError(t, err, "audio moved into the recording folder")
	_, err = os.Stat(legacyAudio)
	assert.True(t, os.IsNotExist(err), "old audio path is gone")
	var stored models.TranscriptionJob
	assert.NoError(t, suite.helper.DB.First(&stored, "id = ?", job.ID).Error)
	assert.Equal(t, newAudio, stored.AudioPath)
	_, err = os.Stat(filepath.Join(folder, "metadata.json"))
	assert.NoError(t, err)
	sums, _ := os.ReadDir(filepath.Join(folder, "summaries"))
	assert.Len(t, sums, 1)

	w := suite.makeAuthenticatedRequest("GET", fmt.Sprintf("/api/v1/transcription/%s/logs", job.ID), nil, false)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "JOB LOG")

	// Running it again changes nothing
	suite.handler.MigrateRecordings(ctx)
	assert.NoError(t, suite.helper.DB.First(&stored, "id = ?", job.ID).Error)
	assert.Equal(t, newAudio, stored.AudioPath)

	// New uploads go straight into their folder
	up := suite.uploadBytes("fresh.m4a", []byte("fresh upload bytes"))
	assert.Equal(t, filepath.Join(root, store.FolderName(up.ID, time.Now()), "audio.m4a"), up.AudioPath)

	// Export zips the recording
	w = suite.makeAuthenticatedRequest("GET", fmt.Sprintf("/api/v1/transcription/%s/export", job.ID), nil, false)
	assert.Equal(t, 200, w.Code)
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if assert.NoError(t, err) {
		names := map[string]bool{}
		for _, f := range zr.File {
			names[f.Name] = true
		}
		base := filepath.Base(folder)
		assert.True(t, names[base+"/audio.wav"], "audio in zip: %v", names)
		assert.True(t, names[base+"/metadata.json"])
		assert.True(t, names[base+"/transcription.log"])
	}

	// Deleting a recording removes its folder
	w = suite.makeAuthenticatedRequest("DELETE", fmt.Sprintf("/api/v1/transcription/%s", job.ID), nil, false)
	assert.Equal(t, 200, w.Code)
	_, err = os.Stat(folder)
	assert.True(t, os.IsNotExist(err), "recording folder removed on delete")

	// A plain <job-id> folder from the first version is renamed to the dated name
	plainJob := suite.helper.CreateTestTranscriptionJob(t, "Plain folder")
	plain := filepath.Join(root, plainJob.ID)
	assert.NoError(t, os.MkdirAll(plain, 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(plain, "audio.wav"), []byte("a"), 0o644))
	assert.NoError(t, suite.helper.DB.Model(&models.TranscriptionJob{}).Where("id = ?", plainJob.ID).Update("audio_path", filepath.Join(plain, "audio.wav")).Error)
	suite.handler.MigrateRecordings(ctx)
	var renamed models.TranscriptionJob
	assert.NoError(t, suite.helper.DB.First(&renamed, "id = ?", plainJob.ID).Error)
	assert.Equal(t, filepath.Join(root, store.FolderName(plainJob.ID, time.Now()), "audio.wav"), renamed.AudioPath)
}

// Test the bulk summary run API (the worker is not started in tests, so
// tasks stay queued)
func (suite *APIHandlerTestSuite) TestSummaryRunAPI() {
	t := suite.T()
	db := suite.helper.DB

	w := suite.makeAuthenticatedRequest("POST", "/api/v1/summaries/run", map[string]interface{}{}, false)
	assert.Equal(t, 400, w.Code, "a selection is required")

	tpl := &models.SummaryTemplate{Name: "Default", Model: "m", Prompt: "p", IsDefault: true}
	assert.NoError(t, db.Create(tpl).Error)
	therapy := &models.SummaryTemplate{Name: "Therapy", Model: "m", Prompt: "p", AutoTags: models.StringList{"therapy"}}
	assert.NoError(t, db.Create(therapy).Error)

	tr := `{"text":"hello"}`
	done := models.TranscriptionJob{ID: "run-done", AudioPath: "a.wav", Status: models.StatusCompleted, Transcript: &tr, Tags: models.StringList{"therapy"}}
	other := models.TranscriptionJob{ID: "run-other", AudioPath: "b.wav", Status: models.StatusCompleted, Transcript: &tr}
	pending := models.TranscriptionJob{ID: "run-pending", AudioPath: "c.wav", Status: models.StatusPending}
	assert.NoError(t, db.Create(&done).Error)
	assert.NoError(t, db.Create(&other).Error)
	assert.NoError(t, db.Create(&pending).Error)

	w = suite.makeAuthenticatedRequest("POST", "/api/v1/summaries/run", map[string]interface{}{"tag": "therapy", "dry_run": true}, false)
	assert.Equal(t, 200, w.Code)
	var dry api.SummaryRunResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &dry))
	assert.Equal(t, []string{"run-done"}, dry.JobIDs)
	assert.Equal(t, 0, dry.Queued)

	w = suite.makeAuthenticatedRequest("POST", "/api/v1/summaries/run", map[string]interface{}{"missing_only": true}, false)
	assert.Equal(t, 202, w.Code)
	var run api.SummaryRunResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &run))
	assert.Equal(t, 2, run.Matched, "only completed, transcribed recordings")
	assert.Equal(t, 2, run.Queued)
	assert.Equal(t, "Default", run.Template)

	var job models.TranscriptionJob
	assert.NoError(t, db.First(&job, "id = ?", "run-done").Error)
	assert.Equal(t, models.SummaryQueued, job.SummaryStatus)

	w = suite.makeAuthenticatedRequest("GET", "/api/v1/summaries/run", nil, false)
	var st api.SummaryRunStatus
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &st))
	assert.True(t, st.Running)
	assert.Equal(t, 2, st.Queued)

	w = suite.makeAuthenticatedRequest("DELETE", "/api/v1/summaries/run", nil, false)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), `"cancelled":2`)
	var cleared models.TranscriptionJob
	assert.NoError(t, db.First(&cleared, "id = ?", "run-done").Error)
	assert.Empty(t, cleared.SummaryStatus)

	// Template auto_tags round-trip through the API, cleaned
	w = suite.makeAuthenticatedRequest("PUT", "/api/v1/summaries/"+therapy.ID, map[string]interface{}{
		"name": "Therapy", "model": "m", "prompt": "p", "auto_tags": []string{" Therapy ", "#Counseling"},
	}, false)
	assert.Equal(t, 200, w.Code)
	var updated models.SummaryTemplate
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
	assert.Equal(t, models.StringList{"therapy", "counseling"}, updated.AutoTags)
}

// Test tag vocabulary settings, the template library and retag selection
func (suite *APIHandlerTestSuite) TestTaggingSettingsAndLibrary() {
	t := suite.T()
	db := suite.helper.DB
	db.Exec("DELETE FROM summary_templates")
	db.Exec("DELETE FROM summary_settings")

	// Defaults are returned before anything is saved
	w := suite.makeAuthenticatedRequest("GET", "/api/v1/summaries/settings", nil, false)
	assert.Equal(t, 200, w.Code)
	var st api.SummarySettingsResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &st))
	assert.True(t, st.TagStrict)
	assert.True(t, st.RedactPII)
	assert.Equal(t, st.DefaultTagTypes, st.TagTypes)
	assert.Equal(t, 9, st.TypeCount)

	// Custom topics are saved; types equal to the default stay default
	w = suite.makeAuthenticatedRequest("POST", "/api/v1/summaries/settings", map[string]interface{}{
		"owner_name": " Jason ", "tag_types": st.DefaultTagTypes, "tag_topics": "gardening: plants\n!money: finances",
	}, false)
	assert.Equal(t, 200, w.Code)
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &st))
	assert.Equal(t, "Jason", st.OwnerName)
	assert.Equal(t, 2, st.TopicCount)
	var saved models.SummarySetting
	assert.NoError(t, db.First(&saved).Error)
	assert.Empty(t, saved.TagTypes, "default list stored as empty")

	w = suite.makeAuthenticatedRequest("POST", "/api/v1/summaries/settings", map[string]interface{}{"tag_topics": "# nothing"}, false)
	assert.Equal(t, 400, w.Code, "an empty topic list is rejected")

	// Built-in templates are added on startup. Older templates with the same
	// or a former name are linked, keeping their content and model.
	ctx := context.Background()
	def := &models.SummaryTemplate{Name: "Default", Model: "qwen3:8b", Prompt: "p"}
	assert.NoError(t, db.Create(def).Error)
	assert.NoError(t, db.Create(&models.SummaryTemplate{Name: "My Notes", Model: "qwen3:8b", Prompt: "mine"}).Error)
	old := &models.SummaryTemplate{Name: "Therapy Discussion - Single", Model: "other", Prompt: "old", Reasoning: true}
	assert.NoError(t, db.Create(old).Error)

	suite.handler.SeedTemplates(ctx)
	suite.handler.SeedTemplates(ctx) // idempotent
	var all []models.SummaryTemplate
	assert.NoError(t, db.Find(&all).Error)
	assert.Len(t, all, 13, "12 built-in (2 linked) plus one custom")

	var linked models.SummaryTemplate
	assert.NoError(t, db.First(&linked, "id = ?", old.ID).Error)
	assert.Equal(t, "individual-therapy", linked.BuiltinKey)
	assert.Equal(t, "old", linked.Prompt, "linking keeps the content")
	assert.NoError(t, db.First(def, "id = ?", def.ID).Error)
	assert.True(t, def.IsDefault, "the built-in Default becomes the default when none is marked")
	var media models.SummaryTemplate
	assert.NoError(t, db.First(&media, "builtin_key = ?", "media-notes").Error)
	assert.Equal(t, "qwen3:8b", media.Model, "new templates use the model in use")
	assert.True(t, media.IsEnabled())

	w = suite.makeAuthenticatedRequest("GET", "/api/v1/summaries/", nil, false)
	assert.Equal(t, 200, w.Code)
	var listed []models.SummaryTemplate
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
	assert.Equal(t, def.ID, listed[0].ID, "default first")
	byKey := map[string]models.SummaryTemplate{}
	for _, it := range listed {
		byKey[it.BuiltinKey] = it
	}
	assert.True(t, byKey["individual-therapy"].Customized)
	assert.False(t, byKey["media-notes"].Customized)

	// Reset restores the shipped content and keeps model and reasoning
	w = suite.makeAuthenticatedRequest("POST", "/api/v1/summaries/"+old.ID+"/reset", nil, false)
	assert.Equal(t, 200, w.Code)
	assert.NoError(t, db.First(&linked, "id = ?", old.ID).Error)
	assert.Equal(t, "Individual Therapy Session", linked.Name)
	assert.Equal(t, models.StringList{"individual therapy"}, linked.AutoTags)
	assert.Equal(t, "other", linked.Model)
	assert.True(t, linked.Reasoning)

	// Enable and disable; the default stays enabled
	w = suite.makeAuthenticatedRequest("PUT", "/api/v1/summaries/"+media.ID+"/enabled", map[string]interface{}{"enabled": false}, false)
	assert.Equal(t, 200, w.Code)
	assert.NoError(t, db.First(&media, "id = ?", media.ID).Error)
	assert.False(t, media.IsEnabled())
	w = suite.makeAuthenticatedRequest("PUT", "/api/v1/summaries/"+def.ID+"/enabled", map[string]interface{}{"enabled": false}, false)
	assert.Equal(t, 400, w.Code)

	// Built-ins cannot be deleted; custom templates can
	w = suite.makeAuthenticatedRequest("DELETE", "/api/v1/summaries/"+media.ID, nil, false)
	assert.Equal(t, 409, w.Code)
	var mine models.SummaryTemplate
	assert.NoError(t, db.First(&mine, "name = ?", "My Notes").Error)
	w = suite.makeAuthenticatedRequest("DELETE", "/api/v1/summaries/"+mine.ID, nil, false)
	assert.Equal(t, 204, w.Code)

	// From nothing: templates are added with the settings model
	db.Exec("DELETE FROM summary_templates")
	w = suite.makeAuthenticatedRequest("POST", "/api/v1/summaries/settings", map[string]interface{}{"default_model": "llama3"}, false)
	assert.Equal(t, 200, w.Code)
	suite.handler.SeedTemplates(ctx)
	var fresh []models.SummaryTemplate
	assert.NoError(t, db.Find(&fresh).Error)
	assert.Len(t, fresh, 12)
	assert.NoError(t, db.First(def, "builtin_key = ?", "default").Error)
	assert.True(t, def.IsDefault)
	assert.Equal(t, "llama3", def.Model)

	// Retag selects summarized recordings and skips hand-edited tags
	tr := `{"text":"hello"}`
	a := models.TranscriptionJob{ID: "retag-a", AudioPath: "a.wav", Status: models.StatusCompleted, Transcript: &tr}
	b := models.TranscriptionJob{ID: "retag-b", AudioPath: "b.wav", Status: models.StatusCompleted, Transcript: &tr, TagsEdited: true}
	c := models.TranscriptionJob{ID: "retag-c", AudioPath: "c.wav", Status: models.StatusCompleted, Transcript: &tr}
	for _, j := range []*models.TranscriptionJob{&a, &b, &c} {
		assert.NoError(t, db.Create(j).Error)
	}
	for _, id := range []string{"retag-a", "retag-b"} {
		assert.NoError(t, db.Create(&models.Summary{TranscriptionID: id, TemplateID: &def.ID, Model: "m", Content: "s"}).Error)
	}
	w = suite.makeAuthenticatedRequest("POST", "/api/v1/summaries/run", map[string]interface{}{"retag": true, "dry_run": true}, false)
	assert.Equal(t, 200, w.Code)
	var dry api.SummaryRunResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &dry))
	assert.Equal(t, []string{"retag-a"}, dry.JobIDs)
	assert.Equal(t, "Retag", dry.Template)

	w = suite.makeAuthenticatedRequest("POST", "/api/v1/summaries/run", map[string]interface{}{"retag": true, "include_edited": true, "dry_run": true}, false)
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &dry))
	assert.ElementsMatch(t, []string{"retag-a", "retag-b"}, dry.JobIDs)
}

// Test error responses for non-existent resources
func (suite *APIHandlerTestSuite) TestNotFoundErrors() {
	endpoints := []string{
		"/api/v1/transcription/nonexistent-job",
		"/api/v1/transcription/nonexistent-job/status",
		"/api/v1/transcription/nonexistent-job/transcript",
		"/api/v1/profiles/nonexistent-profile",
		"/api/v1/notes/nonexistent-note",
	}

	for _, endpoint := range endpoints {
		w := suite.makeAuthenticatedRequest("GET", endpoint, nil, false)
		assert.Equal(suite.T(), 404, w.Code, "Endpoint %s should return 404", endpoint)
	}
}

// Test invalid request data
func (suite *APIHandlerTestSuite) TestInvalidRequestData() {
	// Test invalid JSON for login
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/auth/login", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	suite.router.ServeHTTP(w, req)

	assert.Equal(suite.T(), 400, w.Code)

	// Test missing required fields
	emptyLogin := map[string]string{}
	w = suite.makeAuthenticatedRequest("POST", "/api/v1/auth/login", emptyLogin, false)
	assert.True(suite.T(), w.Code >= 400, "Should return error for empty login data")
}

// Test logout
func (suite *APIHandlerTestSuite) TestLogout() {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/auth/logout", nil)
	suite.router.ServeHTTP(w, req)

	assert.Equal(suite.T(), 200, w.Code)
}

func TestAPIHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(APIHandlerTestSuite))
}
