package skillsource

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// fakes：只覆盖被测路径用到的行为。
type fakeRepo struct {
	created  []Source
	byURL    map[string]Source
	deleted  []int64
	syncErrs map[int64]string
	skills   []MarketSkill
}

var _ Repository = (*fakeRepo)(nil)

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byURL: map[string]Source{}, syncErrs: map[int64]string{}}
}

func (f *fakeRepo) Create(ctx context.Context, name, baseURL string) (Source, error) {
	src := Source{ID: int64(len(f.created) + 1), Name: name, BaseURL: baseURL}
	f.created = append(f.created, src)
	f.byURL[baseURL] = src
	return src, nil
}

func (f *fakeRepo) List(ctx context.Context) ([]Source, error) { return f.created, nil }

func (f *fakeRepo) Get(ctx context.Context, id int64) (Source, error) {
	for _, s := range f.created {
		if s.ID == id {
			return s, nil
		}
	}
	return Source{}, errors.New("not found")
}

func (f *fakeRepo) GetByURL(ctx context.Context, baseURL string) (Source, error) {
	if s, ok := f.byURL[baseURL]; ok {
		return s, nil
	}
	return Source{}, errors.New("not found")
}

func (f *fakeRepo) Delete(ctx context.Context, id int64) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeRepo) MarkSynced(ctx context.Context, id int64) error { return nil }

func (f *fakeRepo) MarkSyncError(ctx context.Context, id int64, msg string) error {
	f.syncErrs[id] = msg
	return nil
}

func (f *fakeRepo) ReplaceSkills(ctx context.Context, sourceID int64, skills []FetchedSkill) error {
	f.skills = make([]MarketSkill, 0, len(skills))
	for _, s := range skills {
		f.skills = append(f.skills, MarketSkill{SourceID: sourceID, Slug: s.Slug, Name: s.Name})
	}
	return nil
}

func (f *fakeRepo) ListMarketSkills(ctx context.Context) ([]MarketSkill, error) { return f.skills, nil }

func (f *fakeRepo) GetMarketSkill(ctx context.Context, sourceID int64, slug string) (MarketSkill, error) {
	for _, s := range f.skills {
		if s.SourceID == sourceID && s.Slug == slug {
			return s, nil
		}
	}
	return MarketSkill{}, errors.New("not found")
}

// 审核台的列表不筛审核状态，只按传入的维度过滤——和真实 SQL 一致。
func (f *fakeRepo) ListMarketSkillsForReview(ctx context.Context, status ReviewStatus, sourceID int64) ([]MarketSkill, error) {
	out := make([]MarketSkill, 0, len(f.skills))
	for _, s := range f.skills {
		if status != "" && s.ReviewStatus != status {
			continue
		}
		if sourceID != 0 && s.SourceID != sourceID {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeRepo) CountByReviewStatus(ctx context.Context) (map[ReviewStatus]int64, error) {
	counts := map[ReviewStatus]int64{}
	for _, s := range f.skills {
		counts[s.ReviewStatus]++
	}
	return counts, nil
}

func (f *fakeRepo) SetReview(ctx context.Context, sourceID int64, slug string, status ReviewStatus, note string, reviewerID int64) error {
	for i, s := range f.skills {
		if s.SourceID == sourceID && s.Slug == slug {
			f.skills[i].ReviewStatus = status
			f.skills[i].ReviewNote = note
			return nil
		}
	}
	return errors.New("not found")
}

type fakeAdmins struct{ admin bool }

func (a fakeAdmins) IsAdmin(ctx context.Context, userID int64) (bool, error) { return a.admin, nil }

type fakeFetcher struct {
	listErr error
	list    []FetchedSkill
}

func (f fakeFetcher) FetchList(ctx context.Context, baseURL string) ([]FetchedSkill, error) {
	return f.list, f.listErr
}

func (f fakeFetcher) FetchDetail(ctx context.Context, baseURL, slug string) (string, *Owner, string, *json.RawMessage, error) {
	return "", nil, "", nil, nil
}

func (f fakeFetcher) FetchVersions(ctx context.Context, baseURL, slug string) ([]SkillVersion, error) {
	return nil, nil
}

func (f fakeFetcher) DownloadZip(ctx context.Context, baseURL, slug, version string) ([]byte, error) {
	return nil, nil
}

func newTestService(admin bool, fetch Fetcher) (*Service, *fakeRepo) {
	repo := newFakeRepo()
	return NewService(repo, fakeAdmins{admin}, fetch, nil), repo
}

func TestCreateNormalizesAndRejectsNonAdmin(t *testing.T) {
	svc, repo := newTestService(true, fakeFetcher{})

	if _, err := svc.Create(context.Background(), 1, "  ", "https://clawhub.ai"); err == nil {
		t.Fatal("empty name should be rejected")
	}
	if _, err := svc.Create(context.Background(), 1, "Clawhub", "ftp://clawhub.ai"); err == nil {
		t.Fatal("non-http scheme should be rejected")
	}
	if _, err := svc.Create(context.Background(), 1, "Clawhub", "https://clawhub.ai/skills"); err == nil {
		t.Fatal("URL with a path should be rejected")
	}

	src, err := svc.Create(context.Background(), 1, "Clawhub", "https://clawhub.ai/")
	if err != nil {
		t.Fatalf("valid source rejected: %v", err)
	}
	if src.BaseURL != "https://clawhub.ai" {
		t.Fatalf("trailing slash not normalized: %q", src.BaseURL)
	}
	if _, err := svc.Create(context.Background(), 1, "Again", "https://clawhub.ai"); err == nil {
		t.Fatal("duplicate base_url should conflict")
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected exactly one source, got %d", len(repo.created))
	}

	nonAdmin, _ := newTestService(false, fakeFetcher{})
	if _, err := nonAdmin.Create(context.Background(), 2, "X", "https://x.example"); err == nil {
		t.Fatal("non-admin create should be forbidden")
	}
}

func TestSyncReplacesCacheAndRecordsError(t *testing.T) {
	svc, repo := newTestService(true, fakeFetcher{list: []FetchedSkill{{Slug: "a", Name: "A"}, {Slug: "b", Name: "B"}}})
	src, err := svc.Create(context.Background(), 1, "S", "https://s.example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Sync(context.Background(), 1, src.ID); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	if len(repo.skills) != 2 {
		t.Fatalf("expected 2 cached skills, got %d", len(repo.skills))
	}

	// 上游挂了：错误要落到 last_sync_error，而不是被吞掉。
	failing, failingRepo := newTestService(true, fakeFetcher{listErr: errors.New("connection refused")})
	fsrc, _ := failing.Create(context.Background(), 1, "S", "https://s.example")
	if _, err := failing.Sync(context.Background(), 1, fsrc.ID); err == nil {
		t.Fatal("upstream failure should surface")
	}
	if msg := failingRepo.syncErrs[fsrc.ID]; msg == "" {
		t.Fatal("sync error should be recorded on the source")
	}
}

// 同步进来的条目默认待审，不能直接出现在用户侧的市场视图里——这是这个
// 功能的全部意义：一个公开源里混着大量低质量条目，默认放行等于没门槛。
// （用户侧的过滤在 SQL 的 WHERE review_status = 'approved' 里，这里验的是
// 领域侧的默认值和状态流转。）
func TestReview_ApprovesSelectedItemsOnly(t *testing.T) {
	svc, repo := newTestService(true, fakeFetcher{list: []FetchedSkill{
		{Slug: "good", Name: "Good"}, {Slug: "junk", Name: "Junk"},
	}})
	src, err := svc.Create(context.Background(), 1, "S", "https://s.example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Sync(context.Background(), 1, src.ID); err != nil {
		t.Fatal(err)
	}

	if err := svc.Review(context.Background(), 1,
		[]ReviewItem{{SourceID: src.ID, Slug: "good"}}, ReviewApproved, "看起来靠谱"); err != nil {
		t.Fatalf("Review: %v", err)
	}

	got, err := repo.GetMarketSkill(context.Background(), src.ID, "good")
	if err != nil {
		t.Fatal(err)
	}
	if got.ReviewStatus != ReviewApproved || got.ReviewNote != "看起来靠谱" {
		t.Fatalf("expected the reviewed item to be approved with its note, got %+v", got)
	}
	// 没点到的那条不能被顺带放行。
	other, _ := repo.GetMarketSkill(context.Background(), src.ID, "junk")
	if other.ReviewStatus == ReviewApproved {
		t.Fatal("reviewing one item must not approve the rest")
	}
}

func TestReview_RejectsUnknownStatus(t *testing.T) {
	svc, _ := newTestService(true, fakeFetcher{})
	err := svc.Review(context.Background(), 1, []ReviewItem{{SourceID: 1, Slug: "x"}}, ReviewStatus("whatever"), "")
	if err == nil {
		t.Fatal("expected an unknown review status to be rejected")
	}
}

func TestReview_RequiresAdmin(t *testing.T) {
	svc, _ := newTestService(false, fakeFetcher{})
	if err := svc.Review(context.Background(), 1, []ReviewItem{{SourceID: 1, Slug: "x"}}, ReviewApproved, ""); err == nil {
		t.Fatal("a non-admin must not be able to approve skills")
	}
	if _, err := svc.ListForReview(context.Background(), 1, "", 0); err == nil {
		t.Fatal("a non-admin must not see the review queue")
	}
}

// 审核只挡住列表是不够的：知道 slug 的人可以直接调详情接口绕过去。
func TestGetMarketSkill_HidesUnapprovedFromNonAdmin(t *testing.T) {
	svc, repo := newTestService(false, fakeFetcher{})
	repo.skills = []MarketSkill{{SourceID: 1, Slug: "junk", Name: "Junk", ReviewStatus: ReviewPending}}

	if _, err := svc.GetMarketSkill(context.Background(), 7, 1, "junk"); err == nil {
		t.Fatal("a pending skill must not be readable by a normal user")
	}

	repo.skills[0].ReviewStatus = ReviewApproved
	if _, err := svc.GetMarketSkill(context.Background(), 7, 1, "junk"); err != nil {
		t.Fatalf("an approved skill should be readable: %v", err)
	}
}
