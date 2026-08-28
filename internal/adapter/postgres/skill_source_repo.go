package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/skillsource"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// txBeginner 来自 resource_repo.go 的既有约定：整体替换缓存需要一个真
// 事务，store.Querier 单独给不了。
type skillSourceTxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// SkillSourceRepository implements skillsource.Repository.
type SkillSourceRepository struct {
	q    store.Querier
	pool skillSourceTxBeginner
}

func NewSkillSourceRepository(q store.Querier, pool skillSourceTxBeginner) *SkillSourceRepository {
	return &SkillSourceRepository{q: q, pool: pool}
}

var _ skillsource.Repository = (*SkillSourceRepository)(nil)

var errSkillSourceNotFound = domain.NotFound(skillsource.CodeSkillSourceNotFound, "Skill 源不存在")
var errMarketSkillNotFound = domain.NotFound(skillsource.CodeMarketSkillNotFound, "市场里没有这个 Skill")

func (r *SkillSourceRepository) sourceFromRow(row store.ListSkillSourcesRow) skillsource.Source {
	src := skillsource.Source{
		ID: row.ID, Name: row.Name, BaseURL: row.BaseUrl, Status: row.Status,
		LastSyncError: textOrEmpty(row.LastSyncError), SkillCount: row.SkillCount,
	}
	if row.LastSyncedAt.Valid {
		src.LastSyncedAt = row.LastSyncedAt.Time
	}
	return src
}

func textOrEmpty(t pgtype.Text) string {
	if t.Valid {
		return t.String
	}
	return ""
}

func (r *SkillSourceRepository) Create(ctx context.Context, name, baseURL string) (skillsource.Source, error) {
	row, err := r.q.CreateSkillSource(ctx, store.CreateSkillSourceParams{Name: name, BaseUrl: baseURL})
	if err != nil {
		return skillsource.Source{}, err
	}
	return skillsource.Source{
		ID: row.ID, Name: row.Name, BaseURL: row.BaseUrl, Status: row.Status,
		LastSyncError: textOrEmpty(row.LastSyncError),
	}, nil
}

func (r *SkillSourceRepository) List(ctx context.Context) ([]skillsource.Source, error) {
	rows, err := r.q.ListSkillSources(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]skillsource.Source, 0, len(rows))
	for _, row := range rows {
		out = append(out, r.sourceFromRow(row))
	}
	return out, nil
}

func (r *SkillSourceRepository) Get(ctx context.Context, id int64) (skillsource.Source, error) {
	row, err := r.q.GetSkillSource(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return skillsource.Source{}, errSkillSourceNotFound
		}
		return skillsource.Source{}, err
	}
	src := skillsource.Source{
		ID: row.ID, Name: row.Name, BaseURL: row.BaseUrl, Status: row.Status,
		LastSyncError: textOrEmpty(row.LastSyncError),
	}
	if row.LastSyncedAt.Valid {
		src.LastSyncedAt = row.LastSyncedAt.Time
	}
	return src, nil
}

func (r *SkillSourceRepository) GetByURL(ctx context.Context, baseURL string) (skillsource.Source, error) {
	row, err := r.q.GetSkillSourceByURL(ctx, baseURL)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return skillsource.Source{}, errSkillSourceNotFound
		}
		return skillsource.Source{}, err
	}
	return skillsource.Source{
		ID: row.ID, Name: row.Name, BaseURL: row.BaseUrl, Status: row.Status,
		LastSyncError: textOrEmpty(row.LastSyncError),
	}, nil
}

func (r *SkillSourceRepository) Delete(ctx context.Context, id int64) error {
	n, err := r.q.DeleteSkillSource(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return errSkillSourceNotFound
	}
	return nil
}

func (r *SkillSourceRepository) MarkSynced(ctx context.Context, id int64) error {
	_, err := r.q.MarkSkillSourceSynced(ctx, id)
	return err
}

func (r *SkillSourceRepository) MarkSyncError(ctx context.Context, id int64, msg string) error {
	_, err := r.q.MarkSkillSourceSyncError(ctx, store.MarkSkillSourceSyncErrorParams{
		ID: id, LastSyncError: pgtype.Text{String: msg, Valid: true},
	})
	return err
}

// ReplaceSkills 在一个事务里整体替换一个源的缓存：upsert 全部条目，再删
// 掉这次没出现的（上游下架）。同步中途失败时旧缓存保持完整。
func (r *SkillSourceRepository) ReplaceSkills(ctx context.Context, sourceID int64, skills []skillsource.FetchedSkill) error {
	if r.pool == nil {
		return errors.New("skill source repository: sync requires a transaction-capable pool")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := store.New(tx)
	keep := make([]string, 0, len(skills))
	for _, s := range skills {
		updated := pgtype.Timestamptz{}
		if !s.UpdatedAt.IsZero() {
			updated = pgtype.Timestamptz{Time: s.UpdatedAt, Valid: true}
		}
		_, err := q.UpsertMarketSkill(ctx, store.UpsertMarketSkillParams{
			SourceID:  sourceID,
			Slug:      s.Slug,
			Name:      s.Name,
			Summary:   pgtype.Text{String: s.Summary, Valid: s.Summary != ""},
			Version:   pgtype.Text{String: s.Version, Valid: s.Version != ""},
			License:   pgtype.Text{String: s.License, Valid: s.License != ""},
			Changelog: pgtype.Text{String: s.Changelog, Valid: s.Changelog != ""},
			Topics:    s.Topics,
			Stars:     s.Stars,
			Downloads: s.Downloads,
			UpdatedAt: updated,
			Raw:       s.Raw,
		})
		if err != nil {
			return err
		}
		keep = append(keep, s.Slug)
	}
	if len(keep) == 0 {
		keep = []string{""} // <> ALL('{''}') 恒真，即全删
	}
	if _, err := q.DeleteStaleMarketSkills(ctx, store.DeleteStaleMarketSkillsParams{
		SourceID: sourceID,
		Column2:  keep,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func marketSkillFromRow(row store.ListMarketSkillsRow) skillsource.MarketSkill {
	ms := skillsource.MarketSkill{
		SourceID: row.SourceID, SourceName: row.SourceName, SourceBaseURL: row.SourceBaseUrl,
		Slug: row.Slug, Name: row.Name,
		Summary: textOrEmpty(row.Summary), Version: textOrEmpty(row.Version),
		License: textOrEmpty(row.License), Changelog: textOrEmpty(row.Changelog),
		Topics: row.Topics, Stars: row.Stars, Downloads: row.Downloads,
		Raw: append([]byte(nil), row.Raw...),
	}
	if row.UpdatedAt.Valid {
		ms.UpdatedAt = row.UpdatedAt.Time
	}
	return ms
}

func (r *SkillSourceRepository) ListMarketSkills(ctx context.Context) ([]skillsource.MarketSkill, error) {
	rows, err := r.q.ListMarketSkills(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]skillsource.MarketSkill, 0, len(rows))
	for _, row := range rows {
		out = append(out, marketSkillFromRow(row))
	}
	return out, nil
}

func (r *SkillSourceRepository) GetMarketSkill(ctx context.Context, sourceID int64, slug string) (skillsource.MarketSkill, error) {
	row, err := r.q.GetMarketSkill(ctx, store.GetMarketSkillParams{SourceID: sourceID, Slug: slug})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return skillsource.MarketSkill{}, errMarketSkillNotFound
		}
		return skillsource.MarketSkill{}, err
	}
	return marketSkillFromRow(store.ListMarketSkillsRow{
		ID: row.ID, SourceID: row.SourceID, Slug: row.Slug, Name: row.Name,
		Summary: row.Summary, Version: row.Version, License: row.License, Changelog: row.Changelog,
		Topics: row.Topics, Stars: row.Stars, Downloads: row.Downloads,
		UpdatedAt: row.UpdatedAt, Raw: row.Raw, SourceName: row.SourceName, SourceBaseUrl: row.SourceBaseUrl,
	}), nil
}
