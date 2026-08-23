package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/domain/marketplace"
)

// The 应用广场 rules — dependency-closure gating, listing_ref ownership,
// snapshot isolation, subscriber counting — are covered by 25 tests in
// internal/domain/marketplace against in-memory fakes. What is left here is
// the transport contract: DTO shape, error-kind to status mapping, and the
// blackbox guarantee as it appears *on the wire*.

// stubMarketplace is a canned backing store: just enough for a handler to
// produce a response. Behaviour lives in the domain tests, so these stubs
// stay deliberately dumb.
type stubMarketplace struct {
	listing    marketplace.Listing
	meta       marketplace.DisplayMeta
	dependents []marketplace.Dependent
	depIssues  []marketplace.DependencyIssue
	subs       map[int64]marketplace.Subscription
	nextSubID  int64
}

func newStubMarketplace() *stubMarketplace {
	return &stubMarketplace{
		listing: marketplace.Listing{
			ID: 1, AuthorID: 1, Kind: marketplace.KindAgent, ResourceID: 10,
			Ref: "architect", Version: "1.0", Visibility: "blackbox",
			Distribution: marketplace.DistributionPublished, PublishedAt: time.Unix(0, 0).UTC(),
		},
		// Deliberately includes a definition-bearing key so the blackbox
		// test below would actually catch a leak if the DTO ever widened.
		meta:      marketplace.DisplayMeta{"display_name": "全栈应用生成器", "description": "从需求到代码"},
		subs:      map[int64]marketplace.Subscription{},
		nextSubID: 1,
	}
}

// ListingRepository
func (s *stubMarketplace) Create(_ context.Context, l marketplace.Listing) (marketplace.Listing, error) {
	l.ID, l.Distribution = 1, marketplace.DistributionPublished
	s.listing = l
	return l, nil
}
func (s *stubMarketplace) GetByID(context.Context, int64) (marketplace.Listing, error) {
	return s.listing, nil
}
func (s *stubMarketplace) GetLatestPublishedByRef(_ context.Context, ref string) (marketplace.Listing, error) {
	if ref != s.listing.Ref {
		return marketplace.Listing{}, marketplace.ErrNotFound
	}
	return s.listing, nil
}
func (s *stubMarketplace) GetByRefAndVersion(context.Context, string, string) (marketplace.Listing, error) {
	return s.listing, nil
}
func (s *stubMarketplace) ListPublishedPage(_ context.Context, kind marketplace.Kind, afterID int64, _ int32) ([]marketplace.Listing, error) {
	if kind != s.listing.Kind || afterID >= s.listing.ID {
		return nil, nil
	}
	l := s.listing
	l.DisplayMeta = s.meta
	return []marketplace.Listing{l}, nil
}
func (s *stubMarketplace) ListVersionHistory(context.Context, string) ([]marketplace.VersionHistoryEntry, error) {
	return []marketplace.VersionHistoryEntry{{Version: "1.0", Changelog: "首个版本"}}, nil
}
func (s *stubMarketplace) SetDistribution(_ context.Context, _ int64, d marketplace.Distribution) error {
	s.listing.Distribution = d
	return nil
}
func (s *stubMarketplace) IncrementSubscriberCount(context.Context, int64) error { return nil }
func (s *stubMarketplace) DecrementSubscriberCount(context.Context, int64) error { return nil }

// SubscriptionRepository
func (s *stubMarketplace) CreateSubscription(_ context.Context, subscriberID, listingID int64) (marketplace.Subscription, error) {
	sub := marketplace.Subscription{ID: s.nextSubID, SubscriberID: subscriberID, ListingID: listingID}
	s.nextSubID++
	s.subs[sub.ID] = sub
	return sub, nil
}
func (s *stubMarketplace) GetByIDForSubscriber(_ context.Context, id, subscriberID int64) (marketplace.Subscription, error) {
	sub, ok := s.subs[id]
	if !ok || sub.SubscriberID != subscriberID {
		return marketplace.Subscription{}, marketplace.ErrNotFound
	}
	return sub, nil
}
func (s *stubMarketplace) GetByListingAndSubscriber(_ context.Context, listingID, subscriberID int64) (marketplace.Subscription, error) {
	for _, sub := range s.subs {
		if sub.ListingID == listingID && sub.SubscriberID == subscriberID {
			return sub, nil
		}
	}
	return marketplace.Subscription{}, marketplace.ErrNotFound
}
func (s *stubMarketplace) GetByRefForSubscriber(context.Context, string, int64) (marketplace.Subscription, error) {
	return marketplace.Subscription{}, marketplace.ErrNotFound
}
func (s *stubMarketplace) ListForSubscriberPage(_ context.Context, subscriberID, afterID int64, _ int32) ([]marketplace.Subscription, error) {
	var out []marketplace.Subscription
	for _, sub := range s.subs {
		if sub.SubscriberID == subscriberID && sub.ID > afterID {
			out = append(out, sub)
		}
	}
	return out, nil
}
func (s *stubMarketplace) Delete(context.Context, int64, int64) error { return nil }
func (s *stubMarketplace) Repoint(_ context.Context, id, _, listingID int64) (marketplace.Subscription, error) {
	sub := s.subs[id]
	sub.ListingID = listingID
	s.subs[id] = sub
	return sub, nil
}

// ResourceCatalog
func (s *stubMarketplace) ResolvePrivateID(context.Context, int64, marketplace.Kind, string, string) (int64, error) {
	return 10, nil
}
func (s *stubMarketplace) SetDisplayMeta(_ context.Context, _ marketplace.Kind, _ int64, meta marketplace.DisplayMeta) error {
	s.meta = meta
	return nil
}
func (s *stubMarketplace) DisplayMetaForListing(context.Context, marketplace.Kind, int64) (marketplace.DisplayMeta, error) {
	return s.meta, nil
}
func (s *stubMarketplace) ConstraintsForListing(context.Context, marketplace.Kind, int64) (*marketplace.ConstraintsSummary, error) {
	calls, timeout := int32(15), int32(120)
	return &marketplace.ConstraintsSummary{MaxToolCalls: &calls, TimeoutSeconds: &timeout, EstimatedTokensRange: "1000~2000"}, nil
}
func (s *stubMarketplace) Freeze(context.Context, marketplace.Kind, int64) error { return nil }
func (s *stubMarketplace) PublishedDependents(context.Context, int64, marketplace.Kind, string) ([]marketplace.Dependent, error) {
	return s.dependents, nil
}

// DependencyValidator / UserDirectory
func (s *stubMarketplace) Validate(context.Context, int64, marketplace.Kind, string, string) ([]marketplace.DependencyIssue, error) {
	return s.depIssues, nil
}
func (s *stubMarketplace) Lookup(_ context.Context, userID int64) (marketplace.Author, error) {
	return marketplace.Author{ID: userID, Email: "author@example.com", DisplayName: "Author"}, nil
}

// subsPort adapts the stub's differently-named Create to the subscription
// port (ListingRepository already claims the Create name).
type subsPort struct{ *stubMarketplace }

func (s subsPort) Create(ctx context.Context, subscriberID, listingID int64) (marketplace.Subscription, error) {
	return s.stubMarketplace.CreateSubscription(ctx, subscriberID, listingID)
}

func newMarketplaceHandlersForTest() (*MarketplaceHandlers, *stubMarketplace) {
	s := newStubMarketplace()
	svc := marketplace.NewService(s, subsPort{s}, s, s, s)
	return NewMarketplaceHandlers(svc), s
}

func doMarketplaceRequest(h http.HandlerFunc, userID int64, method, path string, routeParams map[string]string, body any) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = strings.NewReader(string(b))
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req = req.WithContext(WithUserID(req.Context(), userID))
	if len(routeParams) > 0 {
		rctx := chi.NewRouteContext()
		for k, v := range routeParams {
			rctx.URLParams.Add(k, v)
		}
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}

func TestPublish_Success201(t *testing.T) {
	h, _ := newMarketplaceHandlersForTest()

	w := doMarketplaceRequest(h.Publish, 1, http.MethodPost, "/marketplace/listings", nil, publishRequest{
		ResourceType: "agent", ResourceRef: "architect", Version: "1.0",
		DisplayMeta: json.RawMessage(`{"display_name":"全栈应用生成器","description":"从需求到代码"}`),
	})

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
}

// display_meta that isn't a JSON object is a *shape* problem, so transport
// rejects it before the service is reached.
func TestPublish_NonObjectDisplayMetaIs400(t *testing.T) {
	h, _ := newMarketplaceHandlersForTest()

	w := doMarketplaceRequest(h.Publish, 1, http.MethodPost, "/marketplace/listings", nil, publishRequest{
		ResourceType: "agent", ResourceRef: "architect", Version: "1.0",
		DisplayMeta: json.RawMessage(`"a string"`),
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

// domain.KindInvalid -> 400, with the service's field errors intact.
func TestPublish_MissingRequiredMetaMapsTo400WithDetails(t *testing.T) {
	h, _ := newMarketplaceHandlersForTest()

	w := doMarketplaceRequest(h.Publish, 1, http.MethodPost, "/marketplace/listings", nil, publishRequest{
		ResourceType: "agent", ResourceRef: "architect", Version: "1.0",
		DisplayMeta: json.RawMessage(`{}`),
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	var env Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if len(env.Details) == 0 {
		t.Fatal("field errors must survive into the envelope")
	}
}

// domain.KindUnprocessable -> 422.
func TestPublish_UnpublishedDependencyMapsTo422(t *testing.T) {
	h, s := newMarketplaceHandlersForTest()
	s.depIssues = []marketplace.DependencyIssue{{Field: "agents[0]", Reason: "未发布"}}

	w := doMarketplaceRequest(h.Publish, 1, http.MethodPost, "/marketplace/listings", nil, publishRequest{
		ResourceType: "bundle", ResourceRef: "web-app", Version: "1.0",
		DisplayMeta: json.RawMessage(`{"display_name":"x","description":"y"}`),
	})

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", w.Code, w.Body.String())
	}
	if !containsCode(w.Body.String(), ErrPublishUnpublishedDeps) {
		t.Fatalf("body should carry 70001: %s", w.Body.String())
	}
}

// domain.KindConflict -> 409.
func TestUnpublish_DependedUponMapsTo409(t *testing.T) {
	h, s := newMarketplaceHandlersForTest()
	s.dependents = []marketplace.Dependent{{Kind: marketplace.KindBundle, Ref: "web-app-builder", Version: "2.0"}}

	w := doMarketplaceRequest(h.Unpublish, 1, http.MethodPost, "/marketplace/listings/1/unpublish", map[string]string{"id": "1"}, nil)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
	}
	if !containsCode(w.Body.String(), ErrDependencyStillReferenced) {
		t.Fatalf("body should carry 70007: %s", w.Body.String())
	}
}

func TestUnpublish_Success204(t *testing.T) {
	h, _ := newMarketplaceHandlersForTest()

	w := doMarketplaceRequest(h.Unpublish, 1, http.MethodPost, "/marketplace/listings/1/unpublish", map[string]string{"id": "1"}, nil)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", w.Code, w.Body.String())
	}
}

// A non-numeric path id is a transport-level 400, before any service call.
func TestUnpublish_NonNumericIDIs400(t *testing.T) {
	h, _ := newMarketplaceHandlersForTest()

	w := doMarketplaceRequest(h.Unpublish, 1, http.MethodPost, "/marketplace/listings/abc/unpublish", map[string]string{"id": "abc"}, nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

// domain.KindNotFound -> 404.
func TestDetail_UnknownRefMapsTo404(t *testing.T) {
	h, _ := newMarketplaceHandlersForTest()

	w := doMarketplaceRequest(h.Detail, 1, http.MethodGet, "/marketplace/listings/nope", map[string]string{"ref": "nope"}, nil)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
	if !containsCode(w.Body.String(), ErrListingNotFound) {
		t.Fatalf("body should carry 70002: %s", w.Body.String())
	}
}

func TestSubscribe_Success201(t *testing.T) {
	h, _ := newMarketplaceHandlersForTest()

	w := doMarketplaceRequest(h.Subscribe, 2, http.MethodPost, "/marketplace/listings/1/subscribe", map[string]string{"id": "1"}, nil)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}
}

// The blackbox rule as it appears on the wire. The domain enforces it
// structurally (ResourceCatalog has no method that returns a definition);
// this asserts the serialized JSON of the richest response — detail — carries
// no definition-bearing field, and that the constraints summary IS present,
// since that is the rule's deliberate exception for cost estimation.
func TestBlackbox_DetailJSONCarriesNoDefinition(t *testing.T) {
	h, _ := newMarketplaceHandlersForTest()

	w := doMarketplaceRequest(h.Detail, 2, http.MethodGet, "/marketplace/listings/architect", map[string]string{"ref": "architect"}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	for _, forbidden := range []string{"persona", "definition", "orchestration", "instructions", "human_gates"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("blackbox leak: detail response contains %q\n%s", forbidden, body)
		}
	}
	if !strings.Contains(body, "constraints_summary") {
		t.Errorf("constraints_summary is the blackbox rule's necessary exception and must be present:\n%s", body)
	}
}

// Browse is the other blackbox surface, and the one that fans out over four
// per-kind queries.
func TestBlackbox_BrowseJSONCarriesNoDefinition(t *testing.T) {
	h, _ := newMarketplaceHandlersForTest()

	w := doMarketplaceRequest(h.Browse, 2, http.MethodGet, "/marketplace/listings", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	for _, forbidden := range []string{"persona", "definition", "orchestration", "instructions"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("blackbox leak: browse response contains %q\n%s", forbidden, body)
		}
	}
	if !strings.Contains(body, "display_meta") {
		t.Fatalf("browse should still return display_meta:\n%s", body)
	}
}

func TestBrowse_InvalidResourceTypeMapsTo400(t *testing.T) {
	h, _ := newMarketplaceHandlersForTest()

	w := doMarketplaceRequest(h.Browse, 1, http.MethodGet, "/marketplace/listings?resource_type=nope", nil, nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}
