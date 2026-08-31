// Package web serves the checklist UI.
package web

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"github.com/DecoyOtter/buyforbub/internal/store"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// Server handles every request the app serves.
type Server struct {
	store  *store.Store
	tmpl   *template.Template
	title  string
	log    *slog.Logger
	mux    *http.ServeMux
	static http.Handler
}

// New builds the server. It fails if the templates do not parse.
func New(st *store.Store, title string, log *slog.Logger) (*Server, error) {
	tmpl, err := template.ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("static assets: %w", err)
	}

	s := &Server{
		store:  st,
		tmpl:   tmpl,
		title:  title,
		log:    log,
		mux:    http.NewServeMux(),
		static: http.StripPrefix("/static/", http.FileServerFS(sub)),
	}
	s.routes()
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /{$}", s.handleIndex)
	s.mux.HandleFunc("POST /items", s.handleAdd)
	s.mux.HandleFunc("POST /bundles", s.handleAddBundle)
	s.mux.HandleFunc("GET /items/{id}", s.handleDetail)
	s.mux.HandleFunc("POST /items/{id}", s.handleUpdate)
	s.mux.HandleFunc("GET /items/{id}/row", s.handleRow)
	s.mux.HandleFunc("GET /items/{id}/edit", s.handleEditForm)
	s.mux.HandleFunc("POST /items/{id}/toggle", s.handleToggle)
	s.mux.HandleFunc("POST /items/{id}/delete", s.handleDelete)
	s.mux.HandleFunc("POST /items/{id}/options", s.handleAddOption)
	s.mux.HandleFunc("POST /options/{oid}/choose", s.handleChooseOption)
	s.mux.HandleFunc("POST /options/{oid}/delete", s.handleDeleteOption)
	s.mux.HandleFunc("POST /options/{oid}/comments", s.handleAddComment)
	s.mux.HandleFunc("POST /comments/{cid}/delete", s.handleDeleteComment)
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.Handle("GET /static/", s.static)
}

// --- view models ---

type pageData struct {
	Title      string
	Categories []string
	List       listData
}

type listData struct {
	Groups      []groupData
	Bundles     []bundleData
	BundleItems []bundleItemGroupData
	Overall     store.BudgetSummary
	Done        int
	Total       int
}

type groupData struct {
	Category string
	Items    []itemData
	Summary  store.BudgetSummary
}

type itemData struct {
	store.Item
	ActualCents *int64
}

type bundleData struct {
	store.Bundle
	Members []bundleMemberData
	Chosen  bool
}

func (b bundleData) PriceText() string { return store.FormatMoney(b.PriceCents) }

func (b bundleData) RegularPriceText() string {
	if b.RegularPriceCents == nil {
		return ""
	}
	return store.FormatMoney(*b.RegularPriceCents)
}

func (b bundleData) SavingsText() string {
	if b.RegularPriceCents == nil {
		return ""
	}
	return store.FormatMoney(*b.RegularPriceCents - b.PriceCents)
}

func (b bundleData) SavingsPercent() int64 {
	if b.RegularPriceCents == nil || *b.RegularPriceCents == 0 {
		return 0
	}
	return int64(math.Round(float64((*b.RegularPriceCents-b.PriceCents)*100) / float64(*b.RegularPriceCents)))
}

type bundleMemberData struct {
	ItemName       string
	ComponentLabel string
	ShareCents     int64
}

func (m bundleMemberData) Label() string {
	if m.ComponentLabel != "" {
		return m.ComponentLabel
	}
	return m.ItemName
}

func (m bundleMemberData) ShareText() string { return store.FormatMoney(m.ShareCents) }

type bundleItemGroupData struct {
	Category string
	Items    []bundleItemData
}

type bundleItemData struct {
	ID           int64
	Name         string
	Status       string
	ChosenOption string
}

func (i itemData) ActualText() string {
	if i.ActualCents == nil {
		return ""
	}
	return store.FormatMoney(*i.ActualCents)
}

// detailData backs the expanded panel, and the edit form nested inside it.
type detailData struct {
	Item       store.Item
	Options    []optionView
	Categories []string
	Editing    bool
}

// optionView is an option with its comments attached, since the store returns
// the two separately.
type optionView struct {
	store.Option
	Comments []store.Comment
}

// --- handlers ---

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	list, err := s.listData(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "page", pageData{
		Title:      s.title,
		Categories: store.Categories,
		List:       list,
	})
}

func (s *Server) handleAdd(w http.ResponseWriter, r *http.Request) {
	in, err := parseItemInput(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := s.store.Add(r.Context(), in); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderList(w, r)
}

func (s *Server) handleAddBundle(w http.ResponseWriter, r *http.Request) {
	in, err := parseBundleInput(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := s.store.AddBundle(r.Context(), in); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderList(w, r)
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	in, err := parseItemInput(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := s.store.Update(r.Context(), id, in); err != nil {
		s.fail(w, r, err)
		return
	}
	// The category may have changed, so the whole list is re-rendered.
	s.renderList(w, r)
}

func (s *Server) handleToggle(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	if _, err := s.store.Toggle(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	// Toggling re-sorts the item within its group, so re-render the list.
	s.renderList(w, r)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.store.Delete(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderList(w, r)
}

// handleRow renders the collapsed item, used to close the detail panel.
func (s *Server) handleRow(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	item, err := s.store.Get(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	prices, err := s.store.ChosenOptionPrices(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "row", newItemData(item, prices))
}

func (s *Server) handleDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	s.renderDetail(w, r, id, false)
}

func (s *Server) handleEditForm(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	s.renderDetail(w, r, id, true)
}

func (s *Server) handleAddOption(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.fail(w, r, errBadRequest)
		return
	}
	_, err := s.store.AddOption(r.Context(), id, store.OptionInput{
		URL:   r.PostFormValue("url"),
		Label: r.PostFormValue("label"),
		Price: r.PostFormValue("price"),
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderDetail(w, r, id, false)
}

func (s *Server) handleDeleteOption(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "oid")
	if !ok {
		return
	}
	// Read it first, so the panel can be re-rendered for the right item.
	opt, err := s.store.GetOption(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.store.DeleteOption(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	if opt.Chosen {
		s.renderList(w, r)
		return
	}
	s.renderDetail(w, r, opt.ItemID, false)
}

// handleChooseOption records the option that was bought. That also marks the
// item bought, which re-sorts it, so the whole list is returned.
func (s *Server) handleChooseOption(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "oid")
	if !ok {
		return
	}
	if _, err := s.store.ChooseOption(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderList(w, r)
}

func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "oid")
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.fail(w, r, errBadRequest)
		return
	}
	c, err := s.store.AddComment(r.Context(), id, r.PostFormValue("body"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// The option knows which item's panel to re-render.
	opt, err := s.store.GetOption(r.Context(), c.OptionID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderDetail(w, r, opt.ItemID, false)
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "cid")
	if !ok {
		return
	}
	// Read it first, so the panel can be re-rendered for the right item.
	c, err := s.store.GetComment(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	opt, err := s.store.GetOption(r.Context(), c.OptionID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.store.DeleteComment(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderDetail(w, r, opt.ItemID, false)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok"))
}

// --- helpers ---

func (s *Server) listData(ctx context.Context) (listData, error) {
	items, err := s.store.List(ctx)
	if err != nil {
		return listData{}, err
	}
	prices, err := s.store.ChosenOptionPrices(ctx)
	if err != nil {
		return listData{}, err
	}
	bundles, err := s.store.ListBundles(ctx)
	if err != nil {
		return listData{}, err
	}
	itemsByID := make(map[int64]store.Item, len(items))
	chosenOptionNames := make(map[int64]string, len(items))
	for _, item := range items {
		itemsByID[item.ID] = item
		options, err := s.store.ListOptions(ctx, item.ID)
		if err != nil {
			return listData{}, err
		}
		for _, option := range options {
			if option.Chosen {
				chosenOptionNames[item.ID] = option.Title()
				break
			}
		}
	}
	bundleViews := make([]bundleData, 0, len(bundles))
	for _, bundle := range bundles {
		view := bundleData{Bundle: bundle, Members: make([]bundleMemberData, 0, len(bundle.Members))}
		for _, member := range bundle.Members {
			option, err := s.store.GetOption(ctx, member.OptionID)
			if err != nil {
				return listData{}, err
			}
			item, ok := itemsByID[member.ItemID]
			if !ok || option.PriceCents == nil {
				return listData{}, fmt.Errorf("bundle %d has incomplete member %d", bundle.ID, member.ID)
			}
			view.Members = append(view.Members, bundleMemberData{
				ItemName: item.Name, ComponentLabel: member.ComponentLabel, ShareCents: *option.PriceCents,
			})
			view.Chosen = view.Chosen || option.Chosen
		}
		bundleViews = append(bundleViews, view)
	}
	done, total := store.Progress(items)
	summaries := store.SummarizeBudgets(items, prices)
	groups := store.GroupByCategory(items)
	data := make([]groupData, 0, len(groups))
	for _, group := range groups {
		view := groupData{Category: group.Category, Summary: summaries.Categories[group.Category]}
		view.Items = make([]itemData, 0, len(group.Items))
		for _, item := range group.Items {
			view.Items = append(view.Items, newItemData(item, prices))
		}
		data = append(data, view)
	}
	bundleItemGroups := make([]bundleItemGroupData, 0, len(groups))
	for _, group := range groups {
		view := bundleItemGroupData{Category: group.Category, Items: make([]bundleItemData, 0, len(group.Items))}
		for _, item := range group.Items {
			view.Items = append(view.Items, bundleItemData{
				ID: item.ID, Name: item.Name, Status: item.Status, ChosenOption: chosenOptionNames[item.ID],
			})
		}
		bundleItemGroups = append(bundleItemGroups, view)
	}
	return listData{Groups: data, Bundles: bundleViews, BundleItems: bundleItemGroups, Overall: summaries.Overall, Done: done, Total: total}, nil
}

func newItemData(item store.Item, chosenPrices map[int64]*int64) itemData {
	view := itemData{Item: item}
	if item.Bought() {
		view.ActualCents = chosenPrices[item.ID]
	}
	return view
}

func (s *Server) renderDetail(w http.ResponseWriter, r *http.Request, id int64, editing bool) {
	item, err := s.store.Get(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	opts, err := s.store.ListOptions(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	comments, err := s.store.CommentsByOption(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	views := make([]optionView, 0, len(opts))
	for _, o := range opts {
		views = append(views, optionView{Option: o, Comments: comments[o.ID]})
	}
	s.render(w, r, http.StatusOK, "detail", detailData{
		Item:       item,
		Options:    views,
		Categories: store.Categories,
		Editing:    editing,
	})
}

func (s *Server) renderList(w http.ResponseWriter, r *http.Request) {
	list, err := s.listData(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "list", list)
}

// render buffers the template so a failure mid-render cannot emit a half page.
func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		s.log.ErrorContext(r.Context(), "render template", "template", name, "error", err)
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func parseItemInput(r *http.Request) (store.ItemInput, error) {
	if err := r.ParseForm(); err != nil {
		return store.ItemInput{}, fmt.Errorf("%w: malformed form", errBadRequest)
	}
	// An absent or unparseable qty falls through as 0, which the store
	// normalises to 1.
	qty, _ := strconv.Atoi(r.PostFormValue("qty"))
	return store.ItemInput{
		Name:     r.PostFormValue("name"),
		Qty:      qty,
		Category: r.PostFormValue("category"),
		Notes:    r.PostFormValue("notes"),
		Budget:   r.PostFormValue("budget"),
	}, nil
}

func parseBundleInput(r *http.Request) (store.BundleInput, error) {
	if err := r.ParseForm(); err != nil {
		return store.BundleInput{}, fmt.Errorf("%w: malformed form", errBadRequest)
	}
	members := make([]store.BundleMemberInput, 0, len(r.PostForm["item_id"]))
	for _, rawID := range r.PostForm["item_id"] {
		id, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil {
			return store.BundleInput{}, store.ErrInvalidBundleMembership
		}
		members = append(members, store.BundleMemberInput{
			ItemID:         id,
			ComponentLabel: r.PostFormValue("component_label_" + rawID),
		})
	}
	return store.BundleInput{
		Name:         r.PostFormValue("name"),
		URL:          r.PostFormValue("url"),
		Price:        r.PostFormValue("price"),
		RegularPrice: r.PostFormValue("regular_price"),
		Members:      members,
	}, nil
}

// pathID reads a numeric path segment, writing a 404 if it is not a number.
func (s *Server) pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil {
		http.Error(w, "Not found.", http.StatusNotFound)
		return 0, false
	}
	return id, true
}

var errBadRequest = errors.New("bad request")

// fail maps an error to a status code. Anything unrecognised is a 500 and is
// logged; the user only ever sees a short message.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case errors.Is(err, store.ErrInvalidName):
		http.Error(w, "Give the item a name.", http.StatusBadRequest)
	case errors.Is(err, store.ErrInvalidCategory):
		http.Error(w, "Pick a category.", http.StatusBadRequest)
	case errors.Is(err, store.ErrInvalidStatus):
		http.Error(w, "Unknown status.", http.StatusBadRequest)
	case errors.Is(err, store.ErrInvalidBudget):
		http.Error(w, "Budget should be a number.", http.StatusBadRequest)
	case errors.Is(err, store.ErrInvalidURL):
		http.Error(w, "That does not look like a link.", http.StatusBadRequest)
	case errors.Is(err, store.ErrInvalidPrice):
		http.Error(w, "Price should be a number.", http.StatusBadRequest)
	case errors.Is(err, store.ErrInvalidBundleName):
		http.Error(w, "Give the bundle a name.", http.StatusBadRequest)
	case errors.Is(err, store.ErrInvalidBundlePrice):
		http.Error(w, "Bundle price must be greater than zero with at most two decimal places.", http.StatusBadRequest)
	case errors.Is(err, store.ErrInvalidRegularPrice):
		http.Error(w, "Regular price must be at least the bundle price.", http.StatusBadRequest)
	case errors.Is(err, store.ErrInvalidBundleMembership):
		http.Error(w, "Choose at least two different items.", http.StatusBadRequest)
	case errors.Is(err, store.ErrInvalidComment):
		http.Error(w, "Write something first.", http.StatusBadRequest)
	case errors.Is(err, errBadRequest):
		http.Error(w, "Bad request.", http.StatusBadRequest)
	default:
		s.log.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}
