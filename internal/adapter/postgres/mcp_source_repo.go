package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/mcpsource"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// MCPSourceRepository implements mcpsource.Repository.
type MCPSourceRepository struct {
	q    store.Querier
	pool skillSourceTxBeginner // 同一个"能开事务的池"约定，见 skill_source_repo.go
}

func NewMCPSourceRepository(q store.Querier, pool skillSourceTxBeginner) *MCPSourceRepository {
	return &MCPSourceRepository{q: q, pool: pool}
}

var _ mcpsource.Repository = (*MCPSourceRepository)(nil)

var errMCPSourceNotFound = domain.NotFound(mcpsource.CodeMCPSourceNotFound, "MCP 源不存在")
var errMarketMCPNotFound = domain.NotFound(mcpsource.CodeMarketMCPNotFound, "市场里没有这个 MCP Server")

// sourceFromRow 把一行 mcp_sources 转成领域对象。密钥只转成"配没配"这一
// 个布尔值——密文不进领域对象，就没有哪条路径能把它顺手序列化出去。
func sourceFromMCPRow(row store.McpSource) mcpsource.Source {
	src := mcpsource.Source{
		ID: row.ID, Name: row.Name, BaseURL: row.BaseUrl,
		Protocol: mcpsource.Protocol(row.Protocol), APIPrefix: row.ApiPrefix,
		HasAPIKey: row.ApiKeyEncrypted.Valid && row.ApiKeyEncrypted.String != "",
		Status:    row.Status, LastSyncError: textOrEmpty(row.LastSyncError),
	}
	if row.LastSyncedAt.Valid {
		src.LastSyncedAt = row.LastSyncedAt.Time
	}
	return src
}

func (r *MCPSourceRepository) Create(ctx context.Context, p mcpsource.CreateParams) (mcpsource.Source, error) {
	row, err := r.q.CreateMCPSource(ctx, store.CreateMCPSourceParams{
		Name:            p.Name,
		BaseUrl:         p.BaseURL,
		Protocol:        string(p.Protocol),
		ApiPrefix:       p.APIPrefix,
		ApiKeyEncrypted: pgtype.Text{String: p.EncryptedAPIKey, Valid: p.EncryptedAPIKey != ""},
	})
	if err != nil {
		return mcpsource.Source{}, err
	}
	return sourceFromMCPRow(row), nil
}

// EncryptedAPIKey 只取密文这一列。单独一条查询而不是挂在 Source 上：只有
// 同步那一条路径需要它，列表和详情都不该把密文捞出来。
func (r *MCPSourceRepository) EncryptedAPIKey(ctx context.Context, id int64) (string, error) {
	row, err := r.q.GetMCPSource(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errMCPSourceNotFound
		}
		return "", err
	}
	return textOrEmpty(row.ApiKeyEncrypted), nil
}

func (r *MCPSourceRepository) SetEncryptedAPIKey(ctx context.Context, id int64, encrypted string) error {
	n, err := r.q.SetMCPSourceAPIKey(ctx, store.SetMCPSourceAPIKeyParams{
		ID: id, ApiKeyEncrypted: pgtype.Text{String: encrypted, Valid: encrypted != ""},
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return errMCPSourceNotFound
	}
	return nil
}

func (r *MCPSourceRepository) List(ctx context.Context) ([]mcpsource.Source, error) {
	rows, err := r.q.ListMCPSources(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]mcpsource.Source, 0, len(rows))
	for _, row := range rows {
		src := sourceFromMCPRow(store.McpSource{
			ID: row.ID, Name: row.Name, BaseUrl: row.BaseUrl,
			Protocol: row.Protocol, ApiPrefix: row.ApiPrefix, ApiKeyEncrypted: row.ApiKeyEncrypted,
			Status: row.Status, LastSyncedAt: row.LastSyncedAt, LastSyncError: row.LastSyncError,
		})
		src.ServerCount = row.ServerCount
		out = append(out, src)
	}
	return out, nil
}

func (r *MCPSourceRepository) Get(ctx context.Context, id int64) (mcpsource.Source, error) {
	row, err := r.q.GetMCPSource(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return mcpsource.Source{}, errMCPSourceNotFound
		}
		return mcpsource.Source{}, err
	}
	return sourceFromMCPRow(row), nil
}

func (r *MCPSourceRepository) GetByURL(ctx context.Context, baseURL string) (mcpsource.Source, error) {
	row, err := r.q.GetMCPSourceByURL(ctx, baseURL)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return mcpsource.Source{}, errMCPSourceNotFound
		}
		return mcpsource.Source{}, err
	}
	return sourceFromMCPRow(row), nil
}

func (r *MCPSourceRepository) Delete(ctx context.Context, id int64) error {
	n, err := r.q.DeleteMCPSource(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return errMCPSourceNotFound
	}
	return nil
}

func (r *MCPSourceRepository) MarkSynced(ctx context.Context, id int64) error {
	_, err := r.q.MarkMCPSourceSynced(ctx, id)
	return err
}

func (r *MCPSourceRepository) MarkSyncError(ctx context.Context, id int64, msg string) error {
	_, err := r.q.MarkMCPSourceSyncError(ctx, store.MarkMCPSourceSyncErrorParams{
		ID: id, LastSyncError: pgtype.Text{String: msg, Valid: true},
	})
	return err
}

// ReplaceServers 在一个事务里整体替换一个源的缓存：upsert 全部条目，再删
// 掉这次没出现的（上游下架）。同步中途失败时旧缓存保持完整。
func (r *MCPSourceRepository) ReplaceServers(ctx context.Context, sourceID int64, servers []mcpsource.FetchedServer) error {
	if r.pool == nil {
		return errors.New("mcp source repository: sync requires a transaction-capable pool")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := store.New(tx)
	keep := make([]string, 0, len(servers))
	for _, s := range servers {
		updated := pgtype.Timestamptz{}
		if !s.UpdatedAt.IsZero() {
			updated = pgtype.Timestamptz{Time: s.UpdatedAt, Valid: true}
		}
		topics := s.Topics
		if topics == nil {
			topics = []string{}
		}
		if _, err := q.UpsertMarketMCPServer(ctx, store.UpsertMarketMCPServerParams{
			SourceID:      sourceID,
			Slug:          s.Slug,
			Name:          s.Name,
			Summary:       pgtype.Text{String: s.Summary, Valid: s.Summary != ""},
			Version:       pgtype.Text{String: s.Version, Valid: s.Version != ""},
			License:       pgtype.Text{String: s.License, Valid: s.License != ""},
			RepositoryUrl: pgtype.Text{String: s.RepositoryURL, Valid: s.RepositoryURL != ""},
			RemoteUrl:     pgtype.Text{String: s.RemoteURL, Valid: s.RemoteURL != ""},
			RemoteType:    pgtype.Text{String: s.RemoteType, Valid: s.RemoteType != ""},
			IconUrl:       pgtype.Text{String: s.IconURL, Valid: s.IconURL != ""},
			Topics:        topics,
			UpdatedAt:     updated,
			Raw:           s.Raw,
		}); err != nil {
			return err
		}
		keep = append(keep, s.Slug)
	}
	if len(keep) == 0 {
		keep = []string{""} // <> ALL('{""}') 恒真，即全删
	}
	if _, err := q.DeleteStaleMarketMCPServers(ctx, store.DeleteStaleMarketMCPServersParams{
		SourceID: sourceID,
		Column2:  keep,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// marketServerFromRow 以 ListMarketMCPServersRow 为共同形状——三条查询
// （市场列表、审核列表、单条详情）选出的列一模一样，转换只写一遍。
func marketServerFromRow(row store.ListMarketMCPServersRow) mcpsource.MarketServer {
	ms := mcpsource.MarketServer{
		ID: row.ID, SourceID: row.SourceID,
		SourceName: row.SourceName, SourceBaseURL: row.SourceBaseUrl,
		Slug: row.Slug, Name: row.Name,
		Summary: textOrEmpty(row.Summary), Version: textOrEmpty(row.Version),
		License: textOrEmpty(row.License), RepositoryURL: textOrEmpty(row.RepositoryUrl),
		RemoteURL: textOrEmpty(row.RemoteUrl), RemoteType: textOrEmpty(row.RemoteType),
		IconURL: textOrEmpty(row.IconUrl),
		Topics:  row.Topics, Raw: append([]byte(nil), row.Raw...),
		ReviewStatus: mcpsource.ReviewStatus(row.ReviewStatus),
		ReviewNote:   textOrEmpty(row.ReviewNote),
	}
	if row.UpdatedAt.Valid {
		ms.UpdatedAt = row.UpdatedAt.Time
	}
	if row.ReviewedAt.Valid {
		ms.ReviewedAt = row.ReviewedAt.Time
	}
	if row.SyncedAt.Valid {
		ms.SyncedAt = row.SyncedAt.Time
	}
	if ms.Topics == nil {
		ms.Topics = []string{}
	}
	return ms
}

func (r *MCPSourceRepository) ListMarketServers(ctx context.Context) ([]mcpsource.MarketServer, error) {
	rows, err := r.q.ListMarketMCPServers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]mcpsource.MarketServer, 0, len(rows))
	for _, row := range rows {
		out = append(out, marketServerFromRow(row))
	}
	return out, nil
}

func (r *MCPSourceRepository) GetMarketServer(ctx context.Context, id int64) (mcpsource.MarketServer, error) {
	row, err := r.q.GetMarketMCPServer(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return mcpsource.MarketServer{}, errMarketMCPNotFound
		}
		return mcpsource.MarketServer{}, err
	}
	return marketServerFromRow(store.ListMarketMCPServersRow(row)), nil
}

// reviewFilterMCP 把领域层的 ReviewQuery 翻成 sqlc 的 narg：零值 = 该维度
// 不筛 = NULL。列表和计数用同一套条件，免得两处筛选漂移导致总数和当页对
// 不上。
func reviewFilterMCP(q mcpsource.ReviewQuery) (status, search pgtype.Text, sourceID pgtype.Int8) {
	if q.Status != "" {
		status = pgtype.Text{String: string(q.Status), Valid: true}
	}
	if q.Search != "" {
		search = pgtype.Text{String: q.Search, Valid: true}
	}
	if q.SourceID != 0 {
		sourceID = pgtype.Int8{Int64: q.SourceID, Valid: true}
	}
	return status, search, sourceID
}

func (r *MCPSourceRepository) ListMarketServersForReview(ctx context.Context, q mcpsource.ReviewQuery) ([]mcpsource.MarketServer, error) {
	status, search, sourceID := reviewFilterMCP(q)
	rows, err := r.q.ListMarketMCPServersForReview(ctx, store.ListMarketMCPServersForReviewParams{
		ReviewStatus: status,
		SourceID:     sourceID,
		Search:       search,
		Lim:          int32(q.Limit),
		Off:          int32(q.Offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]mcpsource.MarketServer, 0, len(rows))
	for _, row := range rows {
		out = append(out, marketServerFromRow(store.ListMarketMCPServersRow(row)))
	}
	return out, nil
}

func (r *MCPSourceRepository) CountMarketServersForReview(ctx context.Context, q mcpsource.ReviewQuery) (int64, error) {
	status, search, sourceID := reviewFilterMCP(q)
	return r.q.CountMarketMCPServersForReview(ctx, store.CountMarketMCPServersForReviewParams{
		ReviewStatus: status,
		SourceID:     sourceID,
		Search:       search,
	})
}

func (r *MCPSourceRepository) CountByReviewStatus(ctx context.Context, sourceID int64) (map[mcpsource.ReviewStatus]int64, error) {
	var arg pgtype.Int8
	if sourceID != 0 {
		arg = pgtype.Int8{Int64: sourceID, Valid: true}
	}
	rows, err := r.q.CountMarketMCPServersByReview(ctx, arg)
	if err != nil {
		return nil, err
	}
	out := make(map[mcpsource.ReviewStatus]int64, len(rows))
	for _, row := range rows {
		out[mcpsource.ReviewStatus(row.ReviewStatus)] = row.Count
	}
	return out, nil
}

func (r *MCPSourceRepository) SetReview(ctx context.Context, id int64, status mcpsource.ReviewStatus, note string, reviewerID int64) error {
	n, err := r.q.SetMarketMCPServerReview(ctx, store.SetMarketMCPServerReviewParams{
		ID:           id,
		ReviewStatus: string(status),
		ReviewNote:   pgtype.Text{String: note, Valid: note != ""},
		ReviewedBy:   pgtype.Int8{Int64: reviewerID, Valid: true},
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return errMarketMCPNotFound
	}
	return nil
}
