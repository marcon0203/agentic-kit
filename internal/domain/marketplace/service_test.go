package marketplace_test

import (
	"context"
	"errors"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/marketplace"
)

// mem implements all five marketplace ports over maps. One struct rather
// than five keeps the wiring in each test to a single line; the service only
// ever sees the interfaces.
type mem struct {
	listings      map[int64]marketplace.Listing
	nextListingID int64
	subs          map[int64]marketplace.Subscription
	nextSubID     int64
	history       map[string][]marketplace.VersionHistoryEntry

	privateIDs map[string]int64 // "kind/ref/version" -> resource id
	meta       map[int64]marketplace.DisplayMeta
	frozen     map[string]bool // "kind/resourceID"
	dependents map[string][]marketplace.Dependent
	depIssues  []marketplace.DependencyIssue
	users      map[int64]marketplace.Author
}

func newMem() *mem {
	return &mem{
		listings: map[int64]marketplace.Listing{}, nextListingID: 1,
		subs: map[int64]marketplace.Subscription{}, nextSubID: 1,
		history:    map[string][]marketplace.VersionHistoryEntry{},
		privateIDs: map[string]int64{}, meta: map[int64]marketplace.DisplayMeta{},
		frozen: map[string]bool{}, dependents: map[string][]marketplace.Dependent{},
		users: map[int64]marketplace.Author{},
	}
}

func key(parts ...string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "/"
		}
		out += p
	}
	return out
}

// ── ListingRepository ────────────────────────────────────────────────

func (m *mem) Create(ctx context.Context, l marketplace.Listing) (marketplace.Listing, error) {
	for _, existing := range m.listings {
		if existing.Ref == l.Ref && existing.Version == l.Version {
			return marketplace.Listing{}, marketplace.ErrDuplicate
		}
	}
	l.ID = m.nextListingID
	m.nextListingID++
	l.Distribution = marketplace.DistributionPublished
	m.listings[l.ID] = l
	m.history[l.Ref] = append(m.history[l.Ref], marketplace.VersionHistoryEntry{Version: l.Version, Changelog: l.Changelog})
	return l, nil
}

func (m *mem) GetByID(_ context.Context, id int64) (marketplace.Listing, error) {
	l, ok := m.listings[id]
	if !ok {
		return marketplace.Listing{}, marketplace.ErrNotFound
	}
	return l, nil
}

func (m *mem) GetLatestPublishedByRef(_ context.Context, ref string) (marketplace.Listing, error) {
	var best marketplace.Listing
	found := false
	for _, l := range m.listings {
		if l.Ref == ref && l.Distribution.Published() && (!found || l.ID > best.ID) {
			best, found = l, true
		}
	}
	if !found {
		return marketplace.Listing{}, marketplace.ErrNotFound
	}
	return best, nil
}

func (m *mem) GetByRefAndVersion(_ context.Context, ref, version string) (marketplace.Listing, error) {
	for _, l := range m.listings {
		if l.Ref == ref && l.Version == version {
			return l, nil
		}
	}
	return marketplace.Listing{}, marketplace.ErrNotFound
}

func (m *mem) ListPublishedPage(_ context.Context, kind marketplace.Kind, afterID int64, limit int32) ([]marketplace.Listing, error) {
	var out []marketplace.Listing
	for _, l := range m.listings {
		if l.Kind == kind && l.Distribution.Published() && l.ID > afterID {
			l.DisplayMeta = m.meta[l.ID]
			out = append(out, l)
		}
	}
	for i := 1; i < len(out); i++ { // keep id order, as the real query does
		for j := i; j > 0 && out[j].ID < out[j-1].ID; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if int32(len(out)) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *mem) ListVersionHistory(_ context.Context, ref string) ([]marketplace.VersionHistoryEntry, error) {
	return m.history[ref], nil
}

func (m *mem) SetDistribution(_ context.Context, id int64, d marketplace.Distribution) error {
	l := m.listings[id]
	l.Distribution = d
	m.listings[id] = l
	return nil
}

func (m *mem) IncrementSubscriberCount(_ context.Context, id int64) error {
	l := m.listings[id]
	l.SubscriberCount++
	m.listings[id] = l
	return nil
}

func (m *mem) DecrementSubscriberCount(_ context.Context, id int64) error {
	l := m.listings[id]
	l.SubscriberCount--
	m.listings[id] = l
	return nil
}

// ── SubscriptionRepository ───────────────────────────────────────────

func (m *mem) CreateSub(subscriberID, listingID int64) marketplace.Subscription {
	sub := marketplace.Subscription{ID: m.nextSubID, SubscriberID: subscriberID, ListingID: listingID}
	m.nextSubID++
	m.subs[sub.ID] = sub
	return sub
}

func (m *mem) Create2(ctx context.Context, subscriberID, listingID int64) (marketplace.Subscription, error) {
	return m.CreateSub(subscriberID, listingID), nil
}

func (m *mem) GetByIDForSubscriber(_ context.Context, id, subscriberID int64) (marketplace.Subscription, error) {
	sub, ok := m.subs[id]
	if !ok || sub.SubscriberID != subscriberID {
		return marketplace.Subscription{}, marketplace.ErrNotFound
	}
	return sub, nil
}

func (m *mem) GetByListingAndSubscriber(_ context.Context, listingID, subscriberID int64) (marketplace.Subscription, error) {
	for _, sub := range m.subs {
		if sub.ListingID == listingID && sub.SubscriberID == subscriberID {
			return sub, nil
		}
	}
	return marketplace.Subscription{}, marketplace.ErrNotFound
}

func (m *mem) GetByRefForSubscriber(_ context.Context, ref string, subscriberID int64) (marketplace.Subscription, error) {
	for _, sub := range m.subs {
		if sub.SubscriberID != subscriberID {
			continue
		}
		if l, ok := m.listings[sub.ListingID]; ok && l.Ref == ref {
			return sub, nil
		}
	}
	return marketplace.Subscription{}, marketplace.ErrNotFound
}

func (m *mem) ListForSubscriberPage(_ context.Context, subscriberID, afterID int64, limit int32) ([]marketplace.Subscription, error) {
	var out []marketplace.Subscription
	for _, sub := range m.subs {
		if sub.SubscriberID == subscriberID && sub.ID > afterID {
			out = append(out, sub)
		}
	}
	if int32(len(out)) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *mem) Delete(_ context.Context, id, subscriberID int64) error {
	delete(m.subs, id)
	return nil
}

func (m *mem) Repoint(_ context.Context, id, subscriberID, listingID int64) (marketplace.Subscription, error) {
	sub, ok := m.subs[id]
	if !ok {
		return marketplace.Subscription{}, marketplace.ErrNotFound
	}
	sub.ListingID = listingID
	m.subs[id] = sub
	return sub, nil
}

// ── ResourceCatalog ──────────────────────────────────────────────────

func (m *mem) ResolvePrivateID(_ context.Context, _ int64, kind marketplace.Kind, ref, version string) (int64, error) {
	id, ok := m.privateIDs[key(string(kind), ref, version)]
	if !ok {
		return 0, marketplace.ErrNotFound
	}
	return id, nil
}

func (m *mem) SetDisplayMeta(_ context.Context, _ marketplace.Kind, resourceID int64, meta marketplace.DisplayMeta) error {
	m.meta[resourceID] = meta
	return nil
}

func (m *mem) DisplayMetaForListing(_ context.Context, _ marketplace.Kind, listingID int64) (marketplace.DisplayMeta, error) {
	if meta, ok := m.meta[listingID]; ok {
		return meta, nil
	}
	// Mirror the real adapter: listing display meta is looked up through the
	// listing's resource, so fall back to the listing's own resource id.
	if l, ok := m.listings[listingID]; ok {
		return m.meta[l.ResourceID], nil
	}
	return nil, nil
}

func (m *mem) ConstraintsForListing(_ context.Context, kind marketplace.Kind, _ int64) (*marketplace.ConstraintsSummary, error) {
	if kind == marketplace.KindSkill || kind == marketplace.KindMCP {
		return nil, nil
	}
	timeout := int32(180)
	return &marketplace.ConstraintsSummary{TimeoutSeconds: &timeout}, nil
}

func (m *mem) Freeze(_ context.Context, kind marketplace.Kind, resourceID int64) error {
	m.frozen[key(string(kind), itoa(resourceID))] = true
	return nil
}

func (m *mem) PublishedDependents(_ context.Context, _ int64, kind marketplace.Kind, ref string) ([]marketplace.Dependent, error) {
	return m.dependents[key(string(kind), ref)], nil
}

// ── DependencyValidator / UserDirectory ──────────────────────────────

func (m *mem) Validate(context.Context, int64, marketplace.Kind, string, string) ([]marketplace.DependencyIssue, error) {
	return m.depIssues, nil
}

func (m *mem) Lookup(_ context.Context, userID int64) (marketplace.Author, error) {
	if a, ok := m.users[userID]; ok {
		return a, nil
	}
	return marketplace.Author{ID: userID, DisplayName: "user"}, nil
}

// subsAdapter narrows mem to SubscriptionRepository, whose Create has the
// same name as ListingRepository's but a different signature.
type subsAdapter struct{ *mem }

func (s subsAdapter) Create(ctx context.Context, subscriberID, listingID int64) (marketplace.Subscription, error) {
	return s.mem.Create2(ctx, subscriberID, listingID)
}

func itoa(i int64) string {
	if i == 0 {
		return "0"
	}
	var buf []byte
	for i > 0 {
		buf = append([]byte{byte('0' + i%10)}, buf...)
		i /= 10
	}
	return string(buf)
}

func newSvc(m *mem) *marketplace.Service {
	return marketplace.NewService(m, subsAdapter{m}, m, m, m)
}

func goodMeta() marketplace.DisplayMeta {
	return marketplace.DisplayMeta{"display_name": "全栈应用生成器", "description": "从需求到代码"}
}

func assertErr(t *testing.T, err error, kind domain.Kind, code int) *domain.Error {
	t.Helper()
	var de *domain.Error
	if !errors.As(err, &de) {
		t.Fatalf("expected *domain.Error, got %T: %v", err, err)
	}
	if de.Kind != kind || de.Code != code {
		t.Fatalf("got kind=%v code=%d, want kind=%v code=%d (%v)", de.Kind, de.Code, kind, code, err)
	}
	return de
}

// seedPublishable registers an owned private resource so Publish can resolve
// it.
func seedPublishable(m *mem, kind marketplace.Kind, ref, version string, resourceID int64) {
	m.privateIDs[key(string(kind), ref, version)] = resourceID
}

// ── Publish ──────────────────────────────────────────────────────────

func TestPublish_Success(t *testing.T) {
	m := newMem()
	seedPublishable(m, marketplace.KindAgent, "architect", "1.0", 10)
	svc := newSvc(m)

	view, err := svc.Publish(context.Background(), 1, marketplace.PublishCommand{
		Kind: "agent", Ref: "architect", Version: "1.0", DisplayMeta: goodMeta(),
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if view.Listing.Ref != "architect" || !view.Listing.Distribution.Published() {
		t.Fatalf("unexpected listing %+v", view.Listing)
	}
}

// Every shape problem is reported at once so the author fixes one list.
func TestPublish_CollectsAllFieldErrors(t *testing.T) {
	svc := newSvc(newMem())

	_, err := svc.Publish(context.Background(), 1, marketplace.PublishCommand{
		Kind: "not-a-kind", DisplayMeta: marketplace.DisplayMeta{},
	})

	de := assertErr(t, err, domain.KindInvalid, domain.CodeValidationFailed)
	fields := map[string]bool{}
	for _, d := range de.Details {
		fields[d.Field] = true
	}
	for _, want := range []string{"resource_type", "resource_ref", "version", "display_meta.display_name", "display_meta.description"} {
		if !fields[want] {
			t.Errorf("expected a field error for %q, got %+v", want, de.Details)
		}
	}
}

func TestPublish_UnownedResourceIsNotFound(t *testing.T) {
	svc := newSvc(newMem())

	_, err := svc.Publish(context.Background(), 1, marketplace.PublishCommand{
		Kind: "agent", Ref: "someone-elses", Version: "1.0", DisplayMeta: goodMeta(),
	})

	assertErr(t, err, domain.KindNotFound, domain.CodeResourceNotFound)
}

func TestPublish_UnpublishedDependencyIsUnprocessable(t *testing.T) {
	m := newMem()
	seedPublishable(m, marketplace.KindBundle, "web-app", "1.0", 20)
	m.depIssues = []marketplace.DependencyIssue{
		{Field: "agents[0]", Reason: "依赖路径 web-app → architect@1.0 未发布"},
	}
	svc := newSvc(m)

	_, err := svc.Publish(context.Background(), 1, marketplace.PublishCommand{
		Kind: "bundle", Ref: "web-app", Version: "1.0", DisplayMeta: goodMeta(),
	})

	de := assertErr(t, err, domain.KindUnprocessable, domain.CodePublishUnpublishedDeps)
	if len(de.Details) != 1 || de.Details[0].Field != "agents[0]" {
		t.Fatalf("the failing dependency path must reach the author, got %+v", de.Details)
	}
}

// A cycle gets its own code: publishing the dependency won't fix it, only
// redesigning the graph will.
func TestPublish_CycleGetsItsOwnCode(t *testing.T) {
	m := newMem()
	seedPublishable(m, marketplace.KindBundle, "loop", "1.0", 21)
	m.depIssues = []marketplace.DependencyIssue{{Field: "agents[0]", Reason: "循环", Cycle: true}}
	svc := newSvc(m)

	_, err := svc.Publish(context.Background(), 1, marketplace.PublishCommand{
		Kind: "bundle", Ref: "loop", Version: "1.0", DisplayMeta: goodMeta(),
	})

	assertErr(t, err, domain.KindUnprocessable, domain.CodeCircularDependency)
}

// listing_ref is globally unique across authors, like an npm package name.
func TestPublish_RefOwnedByAnotherAuthorIsConflict(t *testing.T) {
	m := newMem()
	seedPublishable(m, marketplace.KindAgent, "architect", "1.0", 10)
	seedPublishable(m, marketplace.KindAgent, "architect", "2.0", 11)
	svc := newSvc(m)
	ctx := context.Background()

	if _, err := svc.Publish(ctx, 1, marketplace.PublishCommand{Kind: "agent", Ref: "architect", Version: "1.0", DisplayMeta: goodMeta()}); err != nil {
		t.Fatalf("author 1 publish: %v", err)
	}

	_, err := svc.Publish(ctx, 2, marketplace.PublishCommand{Kind: "agent", Ref: "architect", Version: "2.0", DisplayMeta: goodMeta()})

	assertErr(t, err, domain.KindConflict, domain.CodeResourceRefDuplicate)
}

func TestPublish_SameVersionTwiceIsConflict(t *testing.T) {
	m := newMem()
	seedPublishable(m, marketplace.KindAgent, "architect", "1.0", 10)
	svc := newSvc(m)
	ctx := context.Background()
	cmd := marketplace.PublishCommand{Kind: "agent", Ref: "architect", Version: "1.0", DisplayMeta: goodMeta()}

	if _, err := svc.Publish(ctx, 1, cmd); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	_, err := svc.Publish(ctx, 1, cmd)

	assertErr(t, err, domain.KindConflict, domain.CodeResourceRefDuplicate)
}

// ── Unpublish ────────────────────────────────────────────────────────

func TestUnpublish_BlockedWhileDependedUpon(t *testing.T) {
	m := newMem()
	seedPublishable(m, marketplace.KindAgent, "architect", "1.0", 10)
	m.dependents[key("agent", "architect")] = []marketplace.Dependent{
		{Kind: marketplace.KindBundle, Ref: "web-app-builder", Version: "2.0"},
	}
	svc := newSvc(m)
	ctx := context.Background()
	view, _ := svc.Publish(ctx, 1, marketplace.PublishCommand{Kind: "agent", Ref: "architect", Version: "1.0", DisplayMeta: goodMeta()})

	err := svc.Unpublish(ctx, 1, view.Listing.ID)

	de := assertErr(t, err, domain.KindConflict, domain.CodeDependencyStillReferenced)
	if len(de.Details) != 1 || de.Details[0].Reason != "Bundle web-app-builder@2.0 正在依赖" {
		t.Fatalf("the refusal must name the dependent, got %+v", de.Details)
	}
}

func TestUnpublish_AllowedWhenUnreferenced(t *testing.T) {
	m := newMem()
	seedPublishable(m, marketplace.KindAgent, "architect", "1.0", 10)
	svc := newSvc(m)
	ctx := context.Background()
	view, _ := svc.Publish(ctx, 1, marketplace.PublishCommand{Kind: "agent", Ref: "architect", Version: "1.0", DisplayMeta: goodMeta()})

	if err := svc.Unpublish(ctx, 1, view.Listing.ID); err != nil {
		t.Fatalf("unpublish: %v", err)
	}
	if m.listings[view.Listing.ID].Distribution != marketplace.DistributionStopped {
		t.Fatal("expected distribution to be stopped")
	}
}

func TestUnpublish_ByNonAuthorIsForbidden(t *testing.T) {
	m := newMem()
	seedPublishable(m, marketplace.KindAgent, "architect", "1.0", 10)
	svc := newSvc(m)
	ctx := context.Background()
	view, _ := svc.Publish(ctx, 1, marketplace.PublishCommand{Kind: "agent", Ref: "architect", Version: "1.0", DisplayMeta: goodMeta()})

	err := svc.Unpublish(ctx, 999, view.Listing.ID)

	assertErr(t, err, domain.KindForbidden, domain.CodeForbidden)
}

// ── Subscribe ────────────────────────────────────────────────────────

func publishOne(t *testing.T, m *mem, authorID int64, ref, version string, resourceID int64) marketplace.Listing {
	t.Helper()
	seedPublishable(m, marketplace.KindAgent, ref, version, resourceID)
	view, err := newSvc(m).Publish(context.Background(), authorID, marketplace.PublishCommand{
		Kind: "agent", Ref: ref, Version: version, DisplayMeta: goodMeta(),
	})
	if err != nil {
		t.Fatalf("seed publish %s@%s: %v", ref, version, err)
	}
	return view.Listing
}

func TestSubscribe_AuthorCannotSubscribeOwnResource(t *testing.T) {
	m := newMem()
	listing := publishOne(t, m, 1, "architect", "1.0", 10)

	_, err := newSvc(m).Subscribe(context.Background(), 1, listing.ID)

	assertErr(t, err, domain.KindConflict, domain.CodeAlreadySubscribed)
}

// Snapshot isolation's first half: subscribing freezes the exact resource
// version, so the author can never edit what a subscriber committed to.
func TestSubscribe_FreezesTheSubscribedVersion(t *testing.T) {
	m := newMem()
	listing := publishOne(t, m, 1, "architect", "1.0", 10)

	if _, err := newSvc(m).Subscribe(context.Background(), 2, listing.ID); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if !m.frozen[key("agent", "10")] {
		t.Fatal("subscribing must freeze the underlying resource version")
	}
	if got := m.listings[listing.ID].SubscriberCount; got != 1 {
		t.Fatalf("subscriber count = %d, want 1", got)
	}
}

// Subscribing is per listing_ref, not per version — holding two versions at
// once is what upgrade exists to prevent.
func TestSubscribe_SecondVersionOfSameRefIsConflict(t *testing.T) {
	m := newMem()
	v1 := publishOne(t, m, 1, "architect", "1.0", 10)
	v2 := publishOne(t, m, 1, "architect", "2.0", 11)
	svc := newSvc(m)
	ctx := context.Background()

	if _, err := svc.Subscribe(ctx, 2, v1.ID); err != nil {
		t.Fatalf("first subscribe: %v", err)
	}
	_, err := svc.Subscribe(ctx, 2, v2.ID)

	assertErr(t, err, domain.KindConflict, domain.CodeAlreadySubscribed)
}

func TestSubscribe_StoppedListingIsNotFound(t *testing.T) {
	m := newMem()
	listing := publishOne(t, m, 1, "architect", "1.0", 10)
	svc := newSvc(m)
	ctx := context.Background()
	if err := svc.Unpublish(ctx, 1, listing.ID); err != nil {
		t.Fatalf("unpublish: %v", err)
	}

	_, err := svc.Subscribe(ctx, 2, listing.ID)

	assertErr(t, err, domain.KindNotFound, domain.CodeListingNotFound)
}

// ── Upgrade / unsubscribe ────────────────────────────────────────────

// Snapshot isolation's second half: a newer author version is visible as
// LatestVersion but never moves the subscriber until they ask.
func TestUpgrade_IsExplicitAndMovesCounts(t *testing.T) {
	m := newMem()
	v1 := publishOne(t, m, 1, "architect", "1.0", 10)
	v2 := publishOne(t, m, 1, "architect", "2.0", 11)
	svc := newSvc(m)
	ctx := context.Background()

	sub, err := svc.Subscribe(ctx, 2, v1.ID)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	// Still pinned to 1.0, but told 2.0 exists.
	if sub.Listing.Listing.Version != "1.0" || sub.LatestVersion != "2.0" || !sub.HasUpgrade() {
		t.Fatalf("expected pinned 1.0 with 2.0 available, got version=%s latest=%s", sub.Listing.Listing.Version, sub.LatestVersion)
	}

	upgraded, err := svc.Upgrade(ctx, 2, sub.Subscription.ID, "2.0")
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if upgraded.Listing.Listing.Version != "2.0" || upgraded.HasUpgrade() {
		t.Fatalf("expected to be on 2.0 with no further upgrade, got %+v", upgraded.Listing.Listing.Version)
	}
	if !m.frozen[key("agent", "11")] {
		t.Fatal("upgrading must freeze the version being moved onto")
	}
	if m.listings[v1.ID].SubscriberCount != 0 || m.listings[v2.ID].SubscriberCount != 1 {
		t.Fatalf("counts should move: v1=%d v2=%d", m.listings[v1.ID].SubscriberCount, m.listings[v2.ID].SubscriberCount)
	}
}

func TestUpgrade_UnknownTargetVersionIsNotFound(t *testing.T) {
	m := newMem()
	v1 := publishOne(t, m, 1, "architect", "1.0", 10)
	svc := newSvc(m)
	ctx := context.Background()
	sub, _ := svc.Subscribe(ctx, 2, v1.ID)

	_, err := svc.Upgrade(ctx, 2, sub.Subscription.ID, "9.9")

	assertErr(t, err, domain.KindNotFound, domain.CodeListingNotFound)
}

func TestUpgrade_RequiresTargetVersion(t *testing.T) {
	svc := newSvc(newMem())

	_, err := svc.Upgrade(context.Background(), 2, 1, "")

	assertErr(t, err, domain.KindInvalid, domain.CodeValidationFailed)
}

func TestUnsubscribe_DecrementsCount(t *testing.T) {
	m := newMem()
	listing := publishOne(t, m, 1, "architect", "1.0", 10)
	svc := newSvc(m)
	ctx := context.Background()
	sub, _ := svc.Subscribe(ctx, 2, listing.ID)

	if err := svc.Unsubscribe(ctx, 2, sub.Subscription.ID); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	if got := m.listings[listing.ID].SubscriberCount; got != 0 {
		t.Fatalf("subscriber count = %d, want 0", got)
	}
}

func TestUnsubscribe_OfSomeoneElsesSubscriptionIsNotFound(t *testing.T) {
	m := newMem()
	listing := publishOne(t, m, 1, "architect", "1.0", 10)
	svc := newSvc(m)
	ctx := context.Background()
	sub, _ := svc.Subscribe(ctx, 2, listing.ID)

	err := svc.Unsubscribe(ctx, 999, sub.Subscription.ID)

	assertErr(t, err, domain.KindNotFound, domain.CodeResourceNotFound)
}

// ── Browse & detail ──────────────────────────────────────────────────

func TestBrowse_RejectsUnknownResourceType(t *testing.T) {
	_, err := newSvc(newMem()).Browse(context.Background(), 1, marketplace.BrowseQuery{Kind: "nope"})

	assertErr(t, err, domain.KindInvalid, domain.CodeValidationFailed)
}

func TestBrowse_SearchesDisplayMetaCaseInsensitively(t *testing.T) {
	m := newMem()
	listing := publishOne(t, m, 1, "architect", "1.0", 10)
	// Browse reads display meta by listing id.
	m.meta[listing.ID] = marketplace.DisplayMeta{"display_name": "FullStack Builder", "description": "生成代码"}
	svc := newSvc(m)

	hit, err := svc.Browse(context.Background(), 2, marketplace.BrowseQuery{Search: "fullstack"})
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	if len(hit.Items) != 1 {
		t.Fatalf("expected a case-insensitive hit, got %d items", len(hit.Items))
	}

	miss, err := svc.Browse(context.Background(), 2, marketplace.BrowseQuery{Search: "nothing-matches"})
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	if len(miss.Items) != 0 {
		t.Fatalf("expected no hits, got %d", len(miss.Items))
	}
}

func TestDetail_UnknownRefIsNotFound(t *testing.T) {
	_, err := newSvc(newMem()).Detail(context.Background(), 1, "nope")

	assertErr(t, err, domain.KindNotFound, domain.CodeListingNotFound)
}

func TestDetail_ReportsSubscribedForTheViewer(t *testing.T) {
	m := newMem()
	listing := publishOne(t, m, 1, "architect", "1.0", 10)
	svc := newSvc(m)
	ctx := context.Background()
	if _, err := svc.Subscribe(ctx, 2, listing.ID); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	mine, err := svc.Detail(ctx, 2, "architect")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if !mine.Subscribed {
		t.Fatal("the subscriber should see subscribed=true")
	}

	theirs, err := svc.Detail(ctx, 3, "architect")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if theirs.Subscribed {
		t.Fatal("a non-subscriber must see subscribed=false")
	}
}

// The blackbox rule, structurally: ResourceCatalog exposes display meta and
// a constraints summary and nothing else, so no service method can return a
// definition even by mistake. ListingView is the richest thing this context
// produces — this asserts its shape stays free of definition-bearing fields.
func TestListingViewCarriesNoDefinition(t *testing.T) {
	m := newMem()
	listing := publishOne(t, m, 1, "architect", "1.0", 10)
	m.meta[listing.ID] = goodMeta()

	view, err := newSvc(m).Detail(context.Background(), 2, "architect")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}

	// Constraints are the deliberate exception — present, but only cost
	// estimation, never persona or the orchestration graph.
	if view.ConstraintsSummary == nil || view.ConstraintsSummary.TimeoutSeconds == nil {
		t.Fatal("constraints summary should be exposed for cost estimation")
	}
	for k := range view.Listing.DisplayMeta {
		switch k {
		case "persona", "definition", "orchestration", "agents", "instructions":
			t.Fatalf("display meta leaked a definition-bearing key: %q", k)
		}
	}
}

func TestListSubscriptions_PagesAndReportsUpgrades(t *testing.T) {
	m := newMem()
	v1 := publishOne(t, m, 1, "architect", "1.0", 10)
	publishOne(t, m, 1, "architect", "2.0", 11)
	svc := newSvc(m)
	ctx := context.Background()
	if _, err := svc.Subscribe(ctx, 2, v1.ID); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	page, err := svc.ListSubscriptions(ctx, 2, domain.PageQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(page.Items))
	}
	if !page.Items[0].HasUpgrade() {
		t.Fatal("a newer published version should surface as an available upgrade")
	}
}
