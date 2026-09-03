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
	"strings"

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
	s.mux.HandleFunc("GET /bundles/{bid}", s.handleBundleDetail)
	s.mux.HandleFunc("POST /items", s.handleAdd)
	s.mux.HandleFunc("POST /bundles", s.handleAddBundle)
	s.mux.HandleFunc("POST /bundles/{bid}", s.handleUpdateBundle)
	s.mux.HandleFunc("POST /bundles/{bid}/delete", s.handleDeleteBundle)
	s.mux.HandleFunc("POST /bundles/{bid}/choose", s.handleChooseBundle)
	s.mux.HandleFunc("POST /bundles/{bid}/comments", s.handleAddBundleComment)
	s.mux.HandleFunc("POST /bundle-comments/{cid}/delete", s.handleDeleteBundleComment)
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
	AddItem    addItemData
	List       listData
}

type addItemData struct {
	Name       string
	Category   string
	Qty        string
	Categories []string
	Errors     map[string]string
}

type listData struct {
	Categories  []string
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
	Members       []bundleMemberData
	Comments      []store.BundleComment
	EditItems     []bundleItemGroupData
	Chosen        bool
	AffectedItems string
	ChooseConfirm string
}

func (b bundleData) PriceText() string { return store.FormatMoney(b.PriceCents) }

func (b bundleData) PriceInput() string { return centsInput(b.PriceCents) }

func (b bundleData) RegularPriceText() string {
	if b.RegularPriceCents == nil {
		return ""
	}
	return store.FormatMoney(*b.RegularPriceCents)
}

func (b bundleData) RegularPriceInput() string {
	if b.RegularPriceCents == nil {
		return ""
	}
	return centsInput(*b.RegularPriceCents)
}

func centsInput(cents int64) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
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
	ID             int64
	Name           string
	Status         string
	ChosenOption   string
	Selected       bool
	ComponentLabel string
}

func (i itemData) ActualText() string {
	if i.ActualCents == nil {
		return ""
	}
	return store.FormatMoney(*i.ActualCents)
}

// detailData backs the expanded panel, and the edit form nested inside it.
type detailData struct {
	Item        store.Item
	ActualCents *int64
	Options     []optionView
	OptionAdd   optionFormData
	Categories  []string
	Editing     bool
	Edit        editItemData
}

type optionFormData struct {
	URL    string
	Label  string
	Price  string
	Errors map[string]string
}

type editItemData struct {
	Name     string
	Qty      string
	Category string
	Notes    string
	Budget   string
	Errors   map[string]string
}

func (d detailData) ActualText() string {
	if d.ActualCents == nil {
		return ""
	}
	return store.FormatMoney(*d.ActualCents)
}

// optionView is an option with its comments attached, since the store returns
// the two separately.
type optionView struct {
	store.Option
	Comments      []store.Comment
	Bundle        *bundleOptionData
	ChooseConfirm string
	CommentBody   string
	CommentError  string
}

type bundleOptionData struct {
	BundleID       int64
	BundleName     string
	ComponentLabel string
	ItemName       string
	PriceCents     int64
	BundlePrice    int64
	ChooseConfirm  string
	Comments       []store.BundleComment
}

func (b bundleOptionData) Label() string {
	if b.ComponentLabel != "" {
		return b.ComponentLabel
	}
	return b.ItemName
}
func (b bundleOptionData) ShareText() string { return store.FormatMoney(b.PriceCents) }
func (b bundleOptionData) TotalText() string { return store.FormatMoney(b.BundlePrice) }

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
		AddItem:    newAddItemData("", "", "1", nil),
		List:       list,
	})
}

func (s *Server) handleAdd(w http.ResponseWriter, r *http.Request) {
	in, form, err := parseAddItemInput(r)
	if err != nil {
		s.renderAddError(w, r, form, err)
		return
	}
	if _, err := s.store.Add(r.Context(), in); err != nil {
		s.renderAddError(w, r, form, err)
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

func (s *Server) handleBundleDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "bid")
	if !ok {
		return
	}
	s.renderBundleWorkspace(w, r, id)
}

func (s *Server) handleUpdateBundle(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "bid")
	if !ok {
		return
	}
	if _, err := s.store.GetBundle(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	in, err := parseBundleInput(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := s.store.UpdateBundle(r.Context(), id, in); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderList(w, r)
}

func (s *Server) handleDeleteBundle(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "bid")
	if !ok {
		return
	}
	if err := s.store.DeleteBundle(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderList(w, r)
}

func (s *Server) handleChooseBundle(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "bid")
	if !ok {
		return
	}
	if _, err := s.store.ChooseBundle(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderList(w, r)
}

func (s *Server) handleAddBundleComment(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "bid")
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.fail(w, r, errBadRequest)
		return
	}
	if _, err := s.store.AddBundleComment(r.Context(), id, r.PostFormValue("body")); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderList(w, r)
}

func (s *Server) handleDeleteBundleComment(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "cid")
	if !ok {
		return
	}
	if err := s.store.DeleteBundleComment(r.Context(), id); err != nil {
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
	in, form, err := parseEditItemInput(r)
	if err != nil {
		s.renderEditError(w, r, id, form, err)
		return
	}
	if _, err := s.store.Update(r.Context(), id, in); err != nil {
		if len(editItemFieldErrors(err)) == 0 {
			s.fail(w, r, err)
			return
		}
		s.renderEditError(w, r, id, form, err)
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
	form := optionFormData{URL: r.PostFormValue("url"), Label: r.PostFormValue("label"), Price: r.PostFormValue("price")}
	_, err := s.store.AddOption(r.Context(), id, store.OptionInput{URL: form.URL, Label: form.Label, Price: form.Price})
	if err != nil {
		if isHTMX(r) && len(optionFieldErrors(err)) > 0 {
			data, dataErr := s.detailData(r.Context(), id, false)
			if dataErr != nil {
				s.fail(w, r, dataErr)
				return
			}
			form.Errors = optionFieldErrors(err)
			data.OptionAdd = form
			w.Header().Set("HX-Retarget", "#workspace-content")
			w.Header().Set("HX-Reswap", "innerHTML")
			w.Header().Set("HX-Trigger", "option-invalid")
			s.render(w, r, http.StatusOK, "detail", data)
			return
		}
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
	body := r.PostFormValue("body")
	c, err := s.store.AddComment(r.Context(), id, body)
	if err != nil {
		if isHTMX(r) && errors.Is(err, store.ErrInvalidComment) {
			opt, getErr := s.store.GetOption(r.Context(), id)
			if getErr != nil {
				s.fail(w, r, getErr)
				return
			}
			data, dataErr := s.detailData(r.Context(), opt.ItemID, false)
			if dataErr != nil {
				s.fail(w, r, dataErr)
				return
			}
			for i := range data.Options {
				if data.Options[i].ID == id {
					data.Options[i].CommentBody = body
					data.Options[i].CommentError = "Write something first."
					break
				}
			}
			w.Header().Set("HX-Retarget", "#workspace-content")
			w.Header().Set("HX-Reswap", "innerHTML")
			w.Header().Set("HX-Trigger", "comment-invalid")
			s.render(w, r, http.StatusOK, "detail", data)
			return
		}
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
	groups := store.GroupByCategory(items)
	bundleViews := make([]bundleData, 0, len(bundles))
	for _, bundle := range bundles {
		view := bundleData{Bundle: bundle, Members: make([]bundleMemberData, 0, len(bundle.Members))}
		comments, err := s.store.ListBundleComments(ctx, bundle.ID)
		if err != nil {
			return listData{}, err
		}
		view.Comments = comments
		membersByItem := make(map[int64]store.BundleMemberInput, len(bundle.Members))
		affectedNames := make([]string, 0, len(bundle.Members))
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
			membersByItem[member.ItemID] = store.BundleMemberInput{ComponentLabel: member.ComponentLabel}
			affectedNames = append(affectedNames, item.Name)
			view.Chosen = view.Chosen || option.Chosen
		}
		view.AffectedItems = joinNames(affectedNames)
		view.ChooseConfirm = bundleChooseConfirm(bundle, bundles, itemsByID, s.store, ctx)
		view.EditItems = bundleItemsForGroups(groups, chosenOptionNames, membersByItem)
		bundleViews = append(bundleViews, view)
	}
	done, total := store.Progress(items)
	summaries := store.SummarizeBudgets(items, prices)
	data := make([]groupData, 0, len(groups))
	for _, group := range groups {
		view := groupData{Category: group.Category, Summary: summaries.Categories[group.Category]}
		view.Items = make([]itemData, 0, len(group.Items))
		for _, item := range group.Items {
			view.Items = append(view.Items, newItemData(item, prices))
		}
		data = append(data, view)
	}
	bundleItemGroups := bundleItemsForGroups(groups, chosenOptionNames, nil)
	return listData{Categories: store.Categories, Groups: data, Bundles: bundleViews, BundleItems: bundleItemGroups, Overall: summaries.Overall, Done: done, Total: total}, nil
}

func bundleItemsForGroups(groups []store.CategoryGroup, chosen map[int64]string, members map[int64]store.BundleMemberInput) []bundleItemGroupData {
	result := make([]bundleItemGroupData, 0, len(groups))
	for _, group := range groups {
		view := bundleItemGroupData{Category: group.Category, Items: make([]bundleItemData, 0, len(group.Items))}
		for _, item := range group.Items {
			member, selected := members[item.ID]
			view.Items = append(view.Items, bundleItemData{
				ID: item.ID, Name: item.Name, Status: item.Status, ChosenOption: chosen[item.ID], Selected: selected, ComponentLabel: member.ComponentLabel,
			})
		}
		result = append(result, view)
	}
	return result
}

func joinNames(names []string) string {
	if len(names) == 0 {
		return ""
	}
	if len(names) == 1 {
		return names[0]
	}
	if len(names) == 2 {
		return names[0] + " and " + names[1]
	}
	return strings.Join(names[:len(names)-1], ", ") + ", and " + names[len(names)-1]
}

func newItemData(item store.Item, chosenPrices map[int64]*int64) itemData {
	view := itemData{Item: item}
	if item.Bought() {
		view.ActualCents = chosenPrices[item.ID]
	}
	return view
}

func (s *Server) renderDetail(w http.ResponseWriter, r *http.Request, id int64, editing bool) {
	data, err := s.detailData(r.Context(), id, editing)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "detail", data)
}

func (s *Server) renderBundleWorkspace(w http.ResponseWriter, r *http.Request, id int64) {
	list, err := s.listData(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for _, bundle := range list.Bundles {
		if bundle.ID == id {
			s.render(w, r, http.StatusOK, "bundle-detail", bundle)
			return
		}
	}
	s.fail(w, r, store.ErrNotFound)
}

func (s *Server) detailData(ctx context.Context, id int64, editing bool) (detailData, error) {
	item, err := s.store.Get(ctx, id)
	if err != nil {
		return detailData{}, err
	}
	opts, err := s.store.ListOptions(ctx, id)
	if err != nil {
		return detailData{}, err
	}
	comments, err := s.store.CommentsByOption(ctx, id)
	if err != nil {
		return detailData{}, err
	}

	bundleOptions, normalConfirms, err := s.bundleOptionData(ctx)
	if err != nil {
		return detailData{}, err
	}
	views := make([]optionView, 0, len(opts))
	for _, o := range opts {
		bundle := bundleOptions[o.ID]
		views = append(views, optionView{Option: o, Comments: comments[o.ID], Bundle: bundle, ChooseConfirm: normalConfirms[o.ItemID]})
	}
	prices, err := s.store.ChosenOptionPrices(ctx)
	if err != nil {
		return detailData{}, err
	}
	return detailData{
		Item:        item,
		ActualCents: prices[id],
		Options:     views,
		OptionAdd:   newOptionFormData("", "", "", nil),
		Categories:  store.Categories,
		Editing:     editing,
		Edit:        newEditItemData(item),
	}, nil
}

func newOptionFormData(url, label, price string, fieldErrors map[string]string) optionFormData {
	return optionFormData{URL: url, Label: label, Price: price, Errors: fieldErrors}
}

func optionFieldErrors(err error) map[string]string {
	errorsByField := make(map[string]string)
	switch {
	case errors.Is(err, store.ErrInvalidURL):
		errorsByField["url"] = "That does not look like a link."
	case errors.Is(err, store.ErrInvalidPrice):
		errorsByField["price"] = "Price should be a number."
	}
	return errorsByField
}

func newEditItemData(item store.Item) editItemData {
	return editItemData{
		Name: item.Name, Qty: strconv.Itoa(item.Qty), Category: item.Category,
		Notes: item.Notes, Budget: item.BudgetText(), Errors: map[string]string{},
	}
}

func (s *Server) bundleOptionData(ctx context.Context) (map[int64]*bundleOptionData, map[int64]string, error) {
	bundles, err := s.store.ListBundles(ctx)
	if err != nil {
		return nil, nil, err
	}
	items, err := s.store.List(ctx)
	if err != nil {
		return nil, nil, err
	}
	itemsByID := make(map[int64]store.Item, len(items))
	for _, item := range items {
		itemsByID[item.ID] = item
	}
	result := make(map[int64]*bundleOptionData)
	normalConfirms := make(map[int64]string)
	for _, bundle := range bundles {
		comments, err := s.store.ListBundleComments(ctx, bundle.ID)
		if err != nil {
			return nil, nil, err
		}
		confirm := bundleChooseConfirm(bundle, bundles, itemsByID, s.store, ctx)
		chosen := false
		for _, member := range bundle.Members {
			option, err := s.store.GetOption(ctx, member.OptionID)
			if err != nil {
				return nil, nil, err
			}
			chosen = chosen || option.Chosen
			item := itemsByID[member.ItemID]
			if option.PriceCents == nil {
				return nil, nil, fmt.Errorf("bundle %d has incomplete member", bundle.ID)
			}
			result[option.ID] = &bundleOptionData{BundleID: bundle.ID, BundleName: bundle.Name, ComponentLabel: member.ComponentLabel, ItemName: item.Name, PriceCents: *option.PriceCents, BundlePrice: bundle.PriceCents, ChooseConfirm: confirm, Comments: comments}
		}
		if chosen {
			affected := make([]string, 0, len(bundle.Members))
			for _, member := range bundle.Members {
				affected = append(affected, itemsByID[member.ItemID].Name)
			}
			for _, member := range bundle.Members {
				normalConfirms[member.ItemID] = "Choosing this Option will unchoose " + bundle.Name + " and mark " + joinNames(affected) + " needed. Continue?"
			}
		}
	}
	for _, item := range items {
		if _, alreadyConfirmed := normalConfirms[item.ID]; alreadyConfirmed {
			continue
		}
		options, err := s.store.ListOptions(ctx, item.ID)
		if err != nil {
			return nil, nil, err
		}
		for _, option := range options {
			if option.Chosen {
				normalConfirms[item.ID] = "Choosing this Option will replace " + option.Title() + ". Continue?"
				break
			}
		}
	}
	return result, normalConfirms, nil
}

func bundleChooseConfirm(target store.Bundle, bundles []store.Bundle, items map[int64]store.Item, st *store.Store, ctx context.Context) string {
	targetItems := make(map[int64]bool, len(target.Members))
	targetOptions := make(map[int64]bool, len(target.Members))
	affected := make([]string, 0, len(target.Members))
	for _, member := range target.Members {
		targetItems[member.ItemID] = true
		targetOptions[member.OptionID] = true
		affected = append(affected, items[member.ItemID].Name)
	}
	var replaced, conflicts []string
	for itemID := range targetItems {
		options, err := st.ListOptions(ctx, itemID)
		if err != nil {
			continue
		}
		for _, option := range options {
			if option.Chosen && !targetOptions[option.ID] {
				replaced = append(replaced, option.Title())
			}
		}
	}
	for _, bundle := range bundles {
		chosen := false
		sharesTarget := false
		for _, member := range bundle.Members {
			option, err := st.GetOption(ctx, member.OptionID)
			if err != nil {
				continue
			}
			chosen = chosen || option.Chosen
			sharesTarget = sharesTarget || targetItems[member.ItemID]
		}
		if bundle.ID != target.ID && chosen && sharesTarget {
			conflicts = append(conflicts, bundle.Name)
		}
	}
	parts := []string{"Choose " + target.Name + " for " + joinNames(affected) + "."}
	if len(replaced) > 0 {
		parts = append(parts, "This replaces "+joinNames(replaced)+".")
	}
	if len(conflicts) > 0 {
		parts = append(parts, "This unchooses "+joinNames(conflicts)+".")
	}
	return strings.Join(parts, " ") + " Continue?"
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

func (s *Server) renderEditError(w http.ResponseWriter, r *http.Request, id int64, form editItemData, err error) {
	if !isHTMX(r) {
		s.fail(w, r, err)
		return
	}
	data, dataErr := s.detailData(r.Context(), id, true)
	if dataErr != nil {
		s.fail(w, r, dataErr)
		return
	}
	form.Errors = editItemFieldErrors(err)
	data.Edit = form
	w.Header().Set("HX-Retarget", "#workspace-content")
	w.Header().Set("HX-Reswap", "innerHTML")
	w.Header().Set("HX-Trigger", "item-invalid")
	s.render(w, r, http.StatusOK, "detail", data)
}

func editItemFieldErrors(err error) map[string]string {
	errorsByField := make(map[string]string)
	switch {
	case errors.Is(err, store.ErrInvalidName):
		errorsByField["name"] = "Give the item a name."
	case errors.Is(err, store.ErrInvalidCategory):
		errorsByField["category"] = "Pick a category."
	case errors.Is(err, errInvalidQty):
		errorsByField["qty"] = "Qty must be a whole number greater than zero."
	case errors.Is(err, store.ErrInvalidBudget):
		errorsByField["budget"] = "Budget should be a number."
	}
	return errorsByField
}

func parseEditItemInput(r *http.Request) (store.ItemInput, editItemData, error) {
	if err := r.ParseForm(); err != nil {
		return store.ItemInput{}, editItemData{}, fmt.Errorf("%w: malformed form", errBadRequest)
	}
	qtyText := strings.TrimSpace(r.PostFormValue("qty"))
	if qtyText == "" {
		qtyText = "1"
	}
	qty, err := strconv.Atoi(qtyText)
	form := editItemData{
		Name: r.PostFormValue("name"), Qty: qtyText, Category: r.PostFormValue("category"),
		Notes: r.PostFormValue("notes"), Budget: r.PostFormValue("budget"), Errors: map[string]string{},
	}
	if err != nil || qty < 1 {
		return store.ItemInput{Name: form.Name, Category: form.Category, Notes: form.Notes, Budget: form.Budget}, form, errInvalidQty
	}
	return store.ItemInput{
		Name: form.Name, Qty: qty, Category: form.Category, Notes: form.Notes, Budget: form.Budget,
	}, form, nil
}

var errInvalidQty = errors.New("invalid quantity")

func parseAddItemInput(r *http.Request) (store.ItemInput, addItemData, error) {
	if err := r.ParseForm(); err != nil {
		return store.ItemInput{}, newAddItemData("", "", "", nil), fmt.Errorf("%w: malformed form", errBadRequest)
	}
	qtyText := strings.TrimSpace(r.PostFormValue("qty"))
	if qtyText == "" {
		qtyText = "1"
	}
	qty, err := strconv.Atoi(qtyText)
	if err != nil || qty < 1 {
		return store.ItemInput{Name: r.PostFormValue("name"), Category: r.PostFormValue("category")}, newAddItemData(r.PostFormValue("name"), r.PostFormValue("category"), qtyText, nil), errInvalidQty
	}
	return store.ItemInput{
		Name:     r.PostFormValue("name"),
		Qty:      qty,
		Category: r.PostFormValue("category"),
	}, newAddItemData(r.PostFormValue("name"), r.PostFormValue("category"), qtyText, nil), nil
}

func newAddItemData(name, category, qty string, fieldErrors map[string]string) addItemData {
	return addItemData{Name: name, Category: category, Qty: qty, Categories: store.Categories, Errors: fieldErrors}
}

func (s *Server) renderAddError(w http.ResponseWriter, r *http.Request, form addItemData, err error) {
	if !isHTMX(r) {
		s.fail(w, r, err)
		return
	}
	form.Errors = addItemFieldErrors(err)
	w.Header().Set("HX-Retarget", "#add-item-sheet-content")
	w.Header().Set("HX-Reswap", "innerHTML")
	w.Header().Set("HX-Trigger", "add-item-invalid")
	s.render(w, r, http.StatusOK, "add-item", form)
}

func addItemFieldErrors(err error) map[string]string {
	errorsByField := make(map[string]string)
	switch {
	case errors.Is(err, store.ErrInvalidName):
		errorsByField["name"] = "Give the item a name."
	case errors.Is(err, store.ErrInvalidCategory):
		errorsByField["category"] = "Pick a category."
	case errors.Is(err, errInvalidQty):
		errorsByField["qty"] = "Qty must be a whole number greater than zero."
	}
	return errorsByField
}

func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

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
	case errors.Is(err, errInvalidQty):
		http.Error(w, "Qty must be a whole number greater than zero.", http.StatusBadRequest)
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
