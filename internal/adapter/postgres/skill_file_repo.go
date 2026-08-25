package postgres

import (
	"context"

	"github.com/marcon0203/agentic-kit/internal/domain/resource"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// SkillFileRepository implements resource.SkillFileRepository.
type SkillFileRepository struct {
	q store.Querier
}

func NewSkillFileRepository(q store.Querier) *SkillFileRepository { return &SkillFileRepository{q: q} }

var _ resource.SkillFileRepository = (*SkillFileRepository)(nil)

// CreateFiles inserts one row per file. Not wrapped in its own transaction
// — the caller (resource.Service.UploadSkill) already accepts that a
// mid-upload failure can leave a partial index, same as it accepts a
// partial OSS upload; a full saga/rollback across OSS + Postgres is more
// machinery than this needs right now.
func (r *SkillFileRepository) CreateFiles(ctx context.Context, skillID, ownerID int64, files []resource.SkillFile) error {
	for _, f := range files {
		if err := r.q.CreateSkillFile(ctx, store.CreateSkillFileParams{
			SkillID: skillID, OwnerUserID: ownerID,
			Path: f.Path, SizeBytes: f.SizeBytes, ContentType: f.ContentType,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *SkillFileRepository) ListFiles(ctx context.Context, skillID, ownerID int64) ([]resource.SkillFile, error) {
	rows, err := r.q.ListSkillFilesForSkill(ctx, store.ListSkillFilesForSkillParams{SkillID: skillID, OwnerUserID: ownerID})
	if err != nil {
		return nil, err
	}
	out := make([]resource.SkillFile, len(rows))
	for i, row := range rows {
		out[i] = resource.SkillFile{Path: row.Path, SizeBytes: row.SizeBytes, ContentType: row.ContentType}
	}
	return out, nil
}
