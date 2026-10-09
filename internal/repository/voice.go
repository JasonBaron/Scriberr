package repository

import (
	"context"
	"strings"
	"time"

	"scriberr/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// VoiceRepository stores voice prints, voice profiles and their samples.
type VoiceRepository interface {
	SavePrints(ctx context.Context, jobID string, prints []models.VoicePrint) error
	ListPrints(ctx context.Context, jobID string) ([]models.VoicePrint, error)
	JobsWithPrints(ctx context.Context) ([]string, error)

	ListProfiles(ctx context.Context) ([]models.VoiceProfile, error)
	GetProfile(ctx context.Context, id string) (*models.VoiceProfile, error)
	ProfileByName(ctx context.Context, name string) (*models.VoiceProfile, error)
	CreateProfile(ctx context.Context, name string) (*models.VoiceProfile, error)
	SaveProfile(ctx context.Context, p *models.VoiceProfile) error
	DeleteProfile(ctx context.Context, id string) error

	// SetSample makes a recording's speaker a sample of a profile (moving it
	// from any other profile). Returns the profile it was in before, if any.
	SetSample(ctx context.Context, profileID, jobID, speaker string) (previous string, err error)
	// RemoveSample drops a recording's speaker from its profile and returns
	// that profile's ID ("" if it had none).
	RemoveSample(ctx context.Context, jobID, speaker string) (string, error)
	ListSamples(ctx context.Context, profileID string) ([]models.VoiceSample, error)
	SampleSpeakers(ctx context.Context, jobID string) (map[string]string, error) // speaker -> profile

	SetVoiceStatus(ctx context.Context, jobID, status, issues string) error
	ScanCandidates(ctx context.Context, rescan bool) ([]string, error)
	VoiceCounts(ctx context.Context) (map[string]int64, error)
	SetFlag(ctx context.Context, jobID, flag string, on bool) error
	ClearStaleVoiceStatuses(ctx context.Context) (int64, error)
}

type voiceRepository struct{ db *gorm.DB }

// NewVoiceRepository returns the voice repository.
func NewVoiceRepository(db *gorm.DB) VoiceRepository { return &voiceRepository{db: db} }

func (r *voiceRepository) SavePrints(ctx context.Context, jobID string, prints []models.VoicePrint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("transcription_job_id = ?", jobID).Delete(&models.VoicePrint{}).Error; err != nil {
			return err
		}
		for i := range prints {
			prints[i].ID = 0
			prints[i].TranscriptionJobID = jobID
		}
		if len(prints) == 0 {
			return nil
		}
		return tx.Create(&prints).Error
	})
}

func (r *voiceRepository) ListPrints(ctx context.Context, jobID string) ([]models.VoicePrint, error) {
	var out []models.VoicePrint
	err := r.db.WithContext(ctx).Where("transcription_job_id = ?", jobID).Order("speaker").Find(&out).Error
	return out, err
}

func (r *voiceRepository) JobsWithPrints(ctx context.Context) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Model(&models.VoicePrint{}).Distinct().Pluck("transcription_job_id", &ids).Error
	return ids, err
}

func (r *voiceRepository) ListProfiles(ctx context.Context) ([]models.VoiceProfile, error) {
	var out []models.VoiceProfile
	err := r.db.WithContext(ctx).Order("name COLLATE NOCASE").Find(&out).Error
	return out, err
}

func (r *voiceRepository) GetProfile(ctx context.Context, id string) (*models.VoiceProfile, error) {
	var p models.VoiceProfile
	if err := r.db.WithContext(ctx).First(&p, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *voiceRepository) ProfileByName(ctx context.Context, name string) (*models.VoiceProfile, error) {
	var p models.VoiceProfile
	err := r.db.WithContext(ctx).Where("LOWER(name) = ?", strings.ToLower(strings.TrimSpace(name))).First(&p).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *voiceRepository) CreateProfile(ctx context.Context, name string) (*models.VoiceProfile, error) {
	p := &models.VoiceProfile{ID: uuid.New().String(), Name: strings.TrimSpace(name)}
	return p, r.db.WithContext(ctx).Create(p).Error
}

func (r *voiceRepository) SaveProfile(ctx context.Context, p *models.VoiceProfile) error {
	return r.db.WithContext(ctx).Save(p).Error
}

// DeleteProfile removes a profile, its samples, and the speaker names it
// gave by voice. Names given by hand stay.
func (r *voiceRepository) DeleteProfile(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("profile_id = ?", id).Delete(&models.VoiceSample{}).Error; err != nil {
			return err
		}
		if err := tx.Where("voice_profile_id = ? AND source = ?", id, models.MappingVoice).Delete(&models.SpeakerMapping{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.VoiceProfile{}, "id = ?", id).Error
	})
}

func (r *voiceRepository) SetSample(ctx context.Context, profileID, jobID, speaker string) (string, error) {
	var previous string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing models.VoiceSample
		err := tx.Where("transcription_job_id = ? AND speaker = ?", jobID, speaker).First(&existing).Error
		if err == nil {
			if existing.ProfileID == profileID {
				previous = profileID
				return nil
			}
			previous = existing.ProfileID
			return tx.Model(&existing).Update("profile_id", profileID).Error
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}
		return tx.Create(&models.VoiceSample{ProfileID: profileID, TranscriptionJobID: jobID, Speaker: speaker}).Error
	})
	return previous, err
}

func (r *voiceRepository) RemoveSample(ctx context.Context, jobID, speaker string) (string, error) {
	var s models.VoiceSample
	err := r.db.WithContext(ctx).Where("transcription_job_id = ? AND speaker = ?", jobID, speaker).First(&s).Error
	if err == gorm.ErrRecordNotFound {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return s.ProfileID, r.db.WithContext(ctx).Delete(&s).Error
}

func (r *voiceRepository) ListSamples(ctx context.Context, profileID string) ([]models.VoiceSample, error) {
	var out []models.VoiceSample
	err := r.db.WithContext(ctx).Where("profile_id = ?", profileID).Order("created_at").Find(&out).Error
	return out, err
}

func (r *voiceRepository) SampleSpeakers(ctx context.Context, jobID string) (map[string]string, error) {
	var out []models.VoiceSample
	if err := r.db.WithContext(ctx).Where("transcription_job_id = ?", jobID).Find(&out).Error; err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, s := range out {
		m[s.Speaker] = s.ProfileID
	}
	return m, nil
}

func (r *voiceRepository) SetVoiceStatus(ctx context.Context, jobID, status, issues string) error {
	updates := map[string]interface{}{"voice_status": status}
	if status == models.VoiceDone || status == models.VoiceNone || status == models.VoiceFailed {
		now := time.Now()
		updates["voice_scanned_at"] = &now
		updates["voice_issues"] = issues
	}
	return r.db.WithContext(ctx).Model(&models.TranscriptionJob{}).Where("id = ?", jobID).Updates(updates).Error
}

// ScanCandidates lists finished single-track recordings with speakers.
// Without rescan, only those never scanned.
func (r *voiceRepository) ScanCandidates(ctx context.Context, rescan bool) ([]string, error) {
	q := r.db.WithContext(ctx).Model(&models.TranscriptionJob{}).
		Where("status = ? AND transcript IS NOT NULL AND (is_multi_track IS NULL OR is_multi_track = ?)", models.StatusCompleted, false).
		Where("id NOT LIKE ?", "track_%")
	if !rescan {
		q = q.Where("voice_status IS NULL OR voice_status = ''")
	}
	var ids []string
	err := q.Order("created_at ASC").Pluck("id", &ids).Error
	return ids, err
}

func (r *voiceRepository) VoiceCounts(ctx context.Context) (map[string]int64, error) {
	type row struct {
		VoiceStatus string
		N           int64
	}
	var rows []row
	err := r.db.WithContext(ctx).Model(&models.TranscriptionJob{}).
		Select("COALESCE(voice_status, '') AS voice_status, COUNT(*) AS n").
		Where("status = ? AND transcript IS NOT NULL AND id NOT LIKE ?", models.StatusCompleted, "track_%").
		Group("COALESCE(voice_status, '')").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, x := range rows {
		out[x.VoiceStatus] = x.N
	}
	var redo int64
	r.db.WithContext(ctx).Model(&models.TranscriptionJob{}).Where("flags LIKE ?", `%"`+models.FlagRedo+`"%`).Count(&redo)
	out["redo"] = redo
	return out, nil
}

func (r *voiceRepository) SetFlag(ctx context.Context, jobID, flag string, on bool) error {
	var job models.TranscriptionJob
	if err := r.db.WithContext(ctx).Select("id", "flags").First(&job, "id = ?", jobID).Error; err != nil {
		return err
	}
	var next models.StringList
	has := false
	for _, f := range job.Flags {
		if f == flag {
			has = true
			if !on {
				continue
			}
		}
		next = append(next, f)
	}
	if on && !has {
		next = append(next, flag)
	}
	if on == has {
		return nil
	}
	return r.db.WithContext(ctx).Model(&models.TranscriptionJob{}).Where("id = ?", jobID).Update("flags", next).Error
}

// ClearStaleVoiceStatuses resets scans a restart cut short so they run again.
func (r *voiceRepository) ClearStaleVoiceStatuses(ctx context.Context) (int64, error) {
	res := r.db.WithContext(ctx).Model(&models.TranscriptionJob{}).
		Where("voice_status IN ?", []string{models.VoiceQueued, models.VoiceRunning}).
		Update("voice_status", "")
	return res.RowsAffected, res.Error
}
