package marketplace

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

// Service is the 应用广场 application service.
type Service struct {
	listings ListingRepository
	subs     SubscriptionRepository
	catalog  ResourceCatalog
	deps     DependencyValidator
	users    UserDirectory
}

func NewService(
	listings ListingRepository,
	subs SubscriptionRepository,
	catalog ResourceCatalog,
	deps DependencyValidator,
	users UserDirectory,
) *Service {
	return &Service{listings: listings, subs: subs, catalog: catalog, deps: deps, users: users}
}

// ── Publish ──────────────────────────────────────────────────────────

// PublishCommand is a request to publish one resource version.
type PublishCommand struct {
	Kind        string
	Ref         string
	Version     string
	DisplayMeta DisplayMeta
	Changelog   string
}

// validate checks the command's own shape, collecting every problem so the
// author sees one complete list.
func (c PublishCommand) validate() (Kind, []domain.FieldError) {
	var errs []domain.FieldError

	kind, ok := ParseKind(c.Kind)
	if !ok {
		errs = append(errs, domain.FieldError{Field: "resource_type", Reason: "must be one of agent, bundle, skill, mcp"})
	}
	if c.Ref == "" {
		errs = append(errs, domain.FieldError{Field: "resource_ref", Reason: "required"})
	}
	if c.Version == "" {
		errs = append(errs, domain.FieldError{Field: "version", Reason: "required"})
	}
	switch {
	case c.DisplayMeta == nil:
		errs = append(errs, domain.FieldError{Field: "display_meta", Reason: "required"})
	default:
		// Both are mandatory because under blackbox distribution they are
		// the *only* description a subscriber will ever see.
		if c.DisplayMeta.DisplayName() == "" {
			errs = append(errs, domain.FieldError{Field: "display_meta.display_name", Reason: "required"})
		}
		if c.DisplayMeta.Description() == "" {
			errs = append(errs, domain.FieldError{Field: "display_meta.description", Reason: "required"})
		}
	}
	return kind, errs
}

// Publish lists a resource version in the square.
func (s *Service) Publish(ctx context.Context, authorID int64, cmd PublishCommand) (ListingView, error) {
	kind, fieldErrs := cmd.validate()
	if len(fieldErrs) > 0 {
		return ListingView{}, domain.Invalid(domain.CodeValidationFailed, "invalid publish request").WithDetails(fieldErrs...)
	}

	// You can only publish something you own, at a version you actually have.
	resourceID, err := s.catalog.ResolvePrivateID(ctx, authorID, kind, cmd.Ref, cmd.Version)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ListingView{}, domain.NotFound(domain.CodeResourceNotFound, "resource not found")
		}
		return ListingView{}, domain.Internal(err)
	}

	// The whole transitive closure must already be published (spec-08 §1):
	// publishing a Bundle whose Agent is private would hand subscribers a
	// resource they cannot actually run.
	issues, err := s.deps.Validate(ctx, authorID, kind, cmd.Ref, cmd.Version)
	if err != nil {
		return ListingView{}, domain.Internal(err)
	}
	if len(issues) > 0 {
		return ListingView{}, dependencyError(issues)
	}

	// listing_ref is globally unique across authors, like an npm package
	// name — one author must not be able to squat another's published name.
	switch existing, err := s.listings.GetLatestPublishedByRef(ctx, cmd.Ref); {
	case err == nil && existing.AuthorID != authorID:
		return ListingView{}, domain.Conflict(domain.CodeResourceRefDuplicate, "该 listing_ref 已被其他作者占用")
	case err != nil && !errors.Is(err, ErrNotFound):
		return ListingView{}, domain.Internal(err)
	}

	if err := s.catalog.SetDisplayMeta(ctx, kind, resourceID, cmd.DisplayMeta); err != nil {
		return ListingView{}, domain.Internal(err)
	}

	listing, err := s.listings.Create(ctx, Listing{
		AuthorID: authorID, Kind: kind, ResourceID: resourceID,
		Ref: cmd.Ref, Version: cmd.Version, Changelog: cmd.Changelog,
	})
	if err != nil {
		if errors.Is(err, ErrDuplicate) {
			return ListingView{}, domain.Conflict(domain.CodeResourceRefDuplicate, "该资源的这个版本已经发布过")
		}
		return ListingView{}, domain.Internal(err)
	}

	return s.viewOf(ctx, listing, false)
}

// dependencyError turns closure issues into one error. A cycle is reported
// under its own code because the fix is structurally different: an
// unpublished dependency is fixed by publishing it, a cycle only by
// redesigning the graph.
func dependencyError(issues []DependencyIssue) *domain.Error {
	details := make([]domain.FieldError, len(issues))
	cyclic := false
	for i, iss := range issues {
		details[i] = domain.FieldError{Field: iss.Field, Reason: iss.Reason}
		if iss.Cycle {
			cyclic = true
		}
	}
	if cyclic {
		return domain.Unprocessable(domain.CodeCircularDependency, "依赖图中存在循环依赖，无法发布").WithDetails(details...)
	}
	return domain.Unprocessable(domain.CodePublishUnpublishedDeps, "存在未发布的依赖，无法发布").WithDetails(details...)
}

// ── Browse & detail ──────────────────────────────────────────────────

// BrowseQuery filters the square. An empty Kind browses every kind.
type BrowseQuery struct {
	Kind   string
	Search string
	Limit  int
	After  int64
}

// Browse returns a page of published listings.
func (s *Service) Browse(ctx context.Context, viewerID int64, q BrowseQuery) (domain.Page[ListingView], error) {
	limit := domain.PageQuery{Limit: q.Limit}.Normalize().Limit

	kinds := AllKinds
	if q.Kind != "" {
		kind, ok := ParseKind(q.Kind)
		if !ok {
			return domain.Page[ListingView]{}, domain.Invalid(domain.CodeValidationFailed, "invalid resource_type")
		}
		kinds = []Kind{kind}
	}

	var rows []Listing
	for _, kind := range kinds {
		page, err := s.listings.ListPublishedPage(ctx, kind, q.After, int32(limit+1))
		if err != nil {
			return domain.Page[ListingView]{}, domain.Internal(err)
		}
		rows = append(rows, page...)
	}
	// Each per-kind page is already ordered by id, and listings share one
	// sequence, so sorting the merged result by id yields the same order a
	// single cross-kind query would have.
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })

	page := domain.NewPage(rows, limit, func(l Listing) string { return itoa(l.ID) })

	views := make([]ListingView, 0, len(page.Items))
	for _, listing := range page.Items {
		if q.Search != "" && !matchesSearch(listing.DisplayMeta, q.Search) {
			continue
		}
		view, err := s.summaryOf(ctx, listing, viewerID)
		if err != nil {
			return domain.Page[ListingView]{}, err
		}
		views = append(views, view)
	}

	return domain.Page[ListingView]{Items: views, HasMore: page.HasMore, NextCursor: page.NextCursor}, nil
}

// matchesSearch does a case-insensitive substring match over the only two
// fields blackbox distribution lets us search.
func matchesSearch(meta DisplayMeta, q string) bool {
	q = strings.ToLower(q)
	return strings.Contains(strings.ToLower(meta.DisplayName()), q) ||
		strings.Contains(strings.ToLower(meta.Description()), q)
}

// Detail returns the newest still-distributed version of a listing_ref.
func (s *Service) Detail(ctx context.Context, viewerID int64, ref string) (ListingView, error) {
	listing, err := s.listings.GetLatestPublishedByRef(ctx, ref)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ListingView{}, domain.NotFound(domain.CodeListingNotFound, "listing 不存在或已下架")
		}
		return ListingView{}, domain.Internal(err)
	}
	return s.viewOf(ctx, listing, s.isSubscribed(ctx, listing.ID, viewerID))
}

// ── Unpublish ────────────────────────────────────────────────────────

// Unpublish stops distribution. Existing subscribers keep working (spec-08:
// "无影响，继续可用") — this only removes it from the square.
func (s *Service) Unpublish(ctx context.Context, authorID, listingID int64) error {
	listing, err := s.listings.GetByID(ctx, listingID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return domain.NotFound(domain.CodeListingNotFound, "listing 不存在")
		}
		return domain.Internal(err)
	}
	if listing.AuthorID != authorID {
		return domain.Forbidden(domain.CodeForbidden, "forbidden")
	}

	// Refused while another *published* resource still depends on this one:
	// otherwise that resource's dependency closure would silently break for
	// its own subscribers.
	dependents, err := s.catalog.PublishedDependents(ctx, authorID, listing.Kind, listing.Ref)
	if err != nil {
		return domain.Internal(err)
	}
	if len(dependents) > 0 {
		details := make([]domain.FieldError, 0, len(dependents))
		for _, d := range dependents {
			details = append(details, domain.FieldError{
				Field:  "depended_by",
				Reason: dependentLabel(d) + " 正在依赖",
			})
		}
		return domain.Conflict(domain.CodeDependencyStillReferenced, "该资源正被其他已发布资源依赖，无法停止分发").WithDetails(details...)
	}

	if err := s.listings.SetDistribution(ctx, listingID, DistributionStopped); err != nil {
		return domain.Internal(err)
	}
	return nil
}

func dependentLabel(d Dependent) string {
	noun := "Agent"
	if d.Kind == KindBundle {
		noun = "Bundle"
	}
	return noun + " " + d.Ref + "@" + d.Version
}

// ── Subscribe ────────────────────────────────────────────────────────

// Subscribe binds the subscriber to this exact listing version and freezes
// the underlying resource — the two halves of snapshot isolation.
func (s *Service) Subscribe(ctx context.Context, subscriberID, listingID int64) (SubscriptionView, error) {
	listing, err := s.listings.GetByID(ctx, listingID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return SubscriptionView{}, domain.NotFound(domain.CodeListingNotFound, "listing 不存在或已停止分发")
		}
		return SubscriptionView{}, domain.Internal(err)
	}
	if !listing.Distribution.Published() {
		return SubscriptionView{}, domain.NotFound(domain.CodeListingNotFound, "listing 不存在或已停止分发")
	}
	if listing.AuthorID == subscriberID {
		return SubscriptionView{}, domain.Conflict(domain.CodeAlreadySubscribed, "不能订阅自己发布的资源")
	}

	// Subscribing is per listing_ref, not per version: holding two versions
	// of the same resource at once is what upgrade exists to avoid.
	switch _, err := s.subs.GetByRefForSubscriber(ctx, listing.Ref, subscriberID); {
	case err == nil:
		return SubscriptionView{}, domain.Conflict(domain.CodeAlreadySubscribed, "已订阅该版本，如需切换版本请使用升级")
	case !errors.Is(err, ErrNotFound):
		return SubscriptionView{}, domain.Internal(err)
	}

	sub, err := s.subs.Create(ctx, subscriberID, listingID)
	if err != nil {
		if errors.Is(err, ErrDuplicate) {
			return SubscriptionView{}, domain.Conflict(domain.CodeAlreadySubscribed, "已订阅该版本")
		}
		return SubscriptionView{}, domain.Internal(err)
	}
	if err := s.catalog.Freeze(ctx, listing.Kind, listing.ResourceID); err != nil {
		return SubscriptionView{}, domain.Internal(err)
	}
	if err := s.listings.IncrementSubscriberCount(ctx, listingID); err != nil {
		return SubscriptionView{}, domain.Internal(err)
	}
	listing.SubscriberCount++ // reflect the increment without a re-read

	return s.subscriptionView(ctx, sub, listing)
}

// ListSubscriptions returns the subscriber's own subscriptions.
func (s *Service) ListSubscriptions(ctx context.Context, subscriberID int64, q domain.PageQuery) (domain.Page[SubscriptionView], error) {
	q = q.Normalize()
	after := atoi(q.After)

	subs, err := s.subs.ListForSubscriberPage(ctx, subscriberID, after, int32(q.Limit+1))
	if err != nil {
		return domain.Page[SubscriptionView]{}, domain.Internal(err)
	}
	page := domain.NewPage(subs, q.Limit, func(sub Subscription) string { return itoa(sub.ID) })

	views := make([]SubscriptionView, 0, len(page.Items))
	for _, sub := range page.Items {
		listing, err := s.listings.GetByID(ctx, sub.ListingID)
		if err != nil {
			return domain.Page[SubscriptionView]{}, domain.Internal(err)
		}
		view, err := s.subscriptionView(ctx, sub, listing)
		if err != nil {
			return domain.Page[SubscriptionView]{}, err
		}
		views = append(views, view)
	}
	return domain.Page[SubscriptionView]{Items: views, HasMore: page.HasMore, NextCursor: page.NextCursor}, nil
}

// Unsubscribe drops the subscription. Run history stays queryable (spec-08:
// "退订成功（历史运行记录仍可查）") — runs live in their own context and are
// not touched here.
func (s *Service) Unsubscribe(ctx context.Context, subscriberID, subscriptionID int64) error {
	sub, err := s.subs.GetByIDForSubscriber(ctx, subscriptionID, subscriberID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return domain.NotFound(domain.CodeResourceNotFound, "subscription not found")
		}
		return domain.Internal(err)
	}
	if err := s.subs.Delete(ctx, subscriptionID, subscriberID); err != nil {
		return domain.Internal(err)
	}
	if err := s.listings.DecrementSubscriberCount(ctx, sub.ListingID); err != nil {
		return domain.Internal(err)
	}
	return nil
}

// Upgrade repoints a subscription at another version. Always explicit:
// snapshot isolation means a new author version must never move a
// subscriber on its own.
func (s *Service) Upgrade(ctx context.Context, subscriberID, subscriptionID int64, targetVersion string) (SubscriptionView, error) {
	if targetVersion == "" {
		return SubscriptionView{}, domain.Invalid(domain.CodeValidationFailed, "target_version is required")
	}

	sub, err := s.subs.GetByIDForSubscriber(ctx, subscriptionID, subscriberID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return SubscriptionView{}, domain.NotFound(domain.CodeResourceNotFound, "subscription not found")
		}
		return SubscriptionView{}, domain.Internal(err)
	}

	current, err := s.listings.GetByID(ctx, sub.ListingID)
	if err != nil {
		return SubscriptionView{}, domain.Internal(err)
	}

	target, err := s.listings.GetByRefAndVersion(ctx, current.Ref, targetVersion)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return SubscriptionView{}, domain.NotFound(domain.CodeListingNotFound, "目标版本不存在")
		}
		return SubscriptionView{}, domain.Internal(err)
	}

	updated, err := s.subs.Repoint(ctx, subscriptionID, subscriberID, target.ID)
	if err != nil {
		return SubscriptionView{}, domain.Internal(err)
	}
	// Freeze the version being moved onto, for the same reason subscribe
	// does: the subscriber has now committed to it.
	if err := s.catalog.Freeze(ctx, target.Kind, target.ResourceID); err != nil {
		return SubscriptionView{}, domain.Internal(err)
	}
	if err := s.listings.IncrementSubscriberCount(ctx, target.ID); err != nil {
		return SubscriptionView{}, domain.Internal(err)
	}
	if err := s.listings.DecrementSubscriberCount(ctx, current.ID); err != nil {
		return SubscriptionView{}, domain.Internal(err)
	}
	target.SubscriberCount++

	return s.subscriptionView(ctx, updated, target)
}

// ── View assembly ────────────────────────────────────────────────────

// summaryOf enriches a listing for a browse row: author and subscribed flag,
// but no constraints or version history (browse doesn't render them, and
// fetching them per row would be N+1 for nothing).
func (s *Service) summaryOf(ctx context.Context, listing Listing, viewerID int64) (ListingView, error) {
	author, err := s.users.Lookup(ctx, listing.AuthorID)
	if err != nil {
		return ListingView{}, domain.Internal(err)
	}
	return ListingView{
		Listing:    listing,
		Author:     author,
		Subscribed: s.isSubscribed(ctx, listing.ID, viewerID),
	}, nil
}

// viewOf assembles the full detail view.
func (s *Service) viewOf(ctx context.Context, listing Listing, subscribed bool) (ListingView, error) {
	meta, err := s.catalog.DisplayMetaForListing(ctx, listing.Kind, listing.ID)
	if err != nil {
		return ListingView{}, domain.Internal(err)
	}
	listing.DisplayMeta = meta

	author, err := s.users.Lookup(ctx, listing.AuthorID)
	if err != nil {
		return ListingView{}, domain.Internal(err)
	}
	constraints, err := s.catalog.ConstraintsForListing(ctx, listing.Kind, listing.ID)
	if err != nil {
		return ListingView{}, domain.Internal(err)
	}
	versions, err := s.listings.ListVersionHistory(ctx, listing.Ref)
	if err != nil {
		return ListingView{}, domain.Internal(err)
	}

	return ListingView{
		Listing: listing, Author: author,
		ConstraintsSummary: constraints, Versions: versions, Subscribed: subscribed,
	}, nil
}

func (s *Service) subscriptionView(ctx context.Context, sub Subscription, listing Listing) (SubscriptionView, error) {
	view, err := s.viewOf(ctx, listing, true)
	if err != nil {
		return SubscriptionView{}, err
	}

	out := SubscriptionView{Subscription: sub, Listing: view}

	// The author's newest published version drives the "有新版本" prompt.
	// Its absence (everything delisted) is normal, not an error.
	switch latest, err := s.listings.GetLatestPublishedByRef(ctx, listing.Ref); {
	case err == nil:
		out.LatestVersion, out.LatestChangelog = latest.Version, latest.Changelog
	case !errors.Is(err, ErrNotFound):
		return SubscriptionView{}, domain.Internal(err)
	}
	return out, nil
}

// isSubscribed is a display flag, so a lookup failure degrades to false
// rather than failing the whole browse page.
func (s *Service) isSubscribed(ctx context.Context, listingID, viewerID int64) bool {
	_, err := s.subs.GetByListingAndSubscriber(ctx, listingID, viewerID)
	return err == nil
}
