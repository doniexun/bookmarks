package bookmarks

import (
	"fmt"
	"net/http"

	"github.com/bihe/bookmarks/api"
	"github.com/bihe/bookmarks/core"
	"github.com/bihe/bookmarks/security"
	"github.com/bihe/bookmarks/store"
	"github.com/go-chi/chi"
	"github.com/go-chi/render"
)

// --------------------------------------------------------------------------
// validate a given path by checking if the folder-structure is avail in DB
// --------------------------------------------------------------------------

// dbFolderValidator checks for the existence of a 'Folder' item with the
// given 'name' in the given 'path'
type dbFolderValidator struct {
	uow  *store.UnitOfWork
	user string
}

func (d dbFolderValidator) Exists(path, name string) bool {
	_, err := d.uow.FolderByPathName(path, name, d.user)
	if err != nil {
		return false
	}
	return true

}

// --------------------------------------------------------------------------
// Bookmark API
// --------------------------------------------------------------------------

// BookmarkAPI combines the API methods of the bookmarks logic
type BookmarkAPI struct {
	uow *store.UnitOfWork
}

// GetAll retrieves the complete list of bookmarks entries from the store
func (app *BookmarkAPI) GetAll(w http.ResponseWriter, r *http.Request) {
	var err error
	var bookmarks = make([]store.BookmarkItem, 0)
	if bookmarks, err = app.uow.AllBookmarks(user(r).Username); err != nil {
		render.Render(w, r, api.ErrNotFound(err))
		return
	}
	render.Render(w, r, NewBookmarkListResponse(mapBookmarks(bookmarks)))
}

// GetByID returns a single bookmark item, path param :NodeId
func (app *BookmarkAPI) GetByID(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "NodeID")
	if nodeID == "" {
		render.Render(w, r, api.ErrInvalidRequest(fmt.Errorf("missing id to load bookmark")))
		return
	}
	var item *store.BookmarkItem
	var err error
	if item, err = app.uow.BookmarkByID(nodeID, user(r).Username); err != nil {
		render.Render(w, r, api.ErrNotFound(err))
		return
	}
	render.Render(w, r, NewBookmarkResponse(mapBookmark(*item)))
}

// FindByPath returns bookmarks/folders with the given path
// the path to find is provided as a query string
func (app *BookmarkAPI) FindByPath(w http.ResponseWriter, r *http.Request) {
	var err error
	var bookmarks []store.BookmarkItem
	path := r.URL.Query().Get("path")
	if path == "" {
		render.Render(w, r, api.ErrInvalidRequest(fmt.Errorf("no path supplied or missing query-param 'path'")))
		return
	}
	if bookmarks, err = app.uow.BookmarkByPath(path, user(r).Username); err != nil {
		render.Render(w, r, api.ErrNotFound(err))
		return
	}
	render.Render(w, r, NewBookmarkListResponse(mapBookmarks(bookmarks)))
}

// Create will save a new bookmark entry
// the bookmark entry has a given type. If the type is Folder, the URL is just ignored and not saved
// if a bookmark is created with a given path, the method first checks if the path is available.
// This availability check of the path determines if for each path segment a corresponding Folder node is
// available. If the node is missing, the Node or Folder cannot be created for this path
func (app *BookmarkAPI) Create(w http.ResponseWriter, r *http.Request) {
	var bookmark *Bookmark
	data := &BookmarkRequest{}
	if err := render.Bind(r, data); err != nil {
		render.Render(w, r, api.ErrInvalidRequest(err))
		return
	}
	bookmark = data.Bookmark
	// validate the supplied bookmark data for mandatory fields, invalid chars, ...
	if err := bookmark.Validate(); err != nil {
		render.Render(w, r, api.ErrInvalidRequest(err))
		return
	}
	// check if the given folder-structure is available
	if err := ValidatePath(bookmark.Path, dbFolderValidator{uow: app.uow, user: user(r).Username}); err != nil {
		render.Render(w, r, api.ErrInvalidRequest(fmt.Errorf("cannot create item because of missing folder structure: %v", err)))
		return
	}

	if bookmark.DisplayName == "" {
		bookmark.DisplayName = bookmark.URL
	}

	t := store.Node
	url := bookmark.URL
	switch bookmark.Type {
	case Node:
		t = store.Node
	case Folder:
		t = store.Folder
		url = ""
	}

	err := app.uow.CreateBookmark(store.BookmarkItem{
		DisplayName: bookmark.DisplayName,
		Path:        bookmark.Path,
		URL:         url,
		SortOrder:   bookmark.SortOrder,
		Type:        t,
		Username:    user(r).Username,
	})
	if err != nil {
		render.Render(w, r, api.ErrInvalidRequest(err))
		return
	}
	render.Render(w, r, api.SuccessResult(http.StatusCreated, fmt.Sprintf("bookmark item created: p:%s, n:%s", bookmark.Path, bookmark.DisplayName)))
}

// Update a bookmark item with new values. The type of the bookmark Node/Folder is not updated.
// It does not make any sense to change a bookmark Node with URL to a Folder
func (app *BookmarkAPI) Update(w http.ResponseWriter, r *http.Request) {
	var bookmark *Bookmark
	data := &BookmarkRequest{}
	if err := render.Bind(r, data); err != nil {
		render.Render(w, r, api.ErrInvalidRequest(err))
		return
	}
	if data.Bookmark.NodeID == "" {
		render.Render(w, r, api.ErrInvalidRequest(fmt.Errorf("cannot upate bookmark with empty ID")))
		return
	}
	bookmark = data.Bookmark

	if _, err := app.uow.BookmarkByID(bookmark.NodeID, user(r).Username); err != nil {
		render.Render(w, r, api.ErrNotFound(fmt.Errorf("bookmark with ID '%s' not available", bookmark.NodeID)))
		return
	}
	// validate the supplied bookmark data for mandatory fields, invalid chars, ...
	if err := bookmark.Validate(); err != nil {
		render.Render(w, r, api.ErrInvalidRequest(err))
		return
	}
	// check if the given folder-structure is available
	if err := ValidatePath(bookmark.Path, dbFolderValidator{uow: app.uow, user: user(r).Username}); err != nil {
		render.Render(w, r, api.ErrInvalidRequest(fmt.Errorf("cannot update item because of missing folder structure: %v", err)))
		return
	}
	if bookmark.DisplayName == "" {
		bookmark.DisplayName = bookmark.URL
	}
	// the URL for a Folder does not make any sense
	url := bookmark.URL
	if bookmark.Type == Folder {
		url = ""
	}
	err := app.uow.UpdateBookmark(store.BookmarkItem{
		ItemID:      bookmark.NodeID,
		DisplayName: bookmark.DisplayName,
		Path:        bookmark.Path,
		URL:         url,
		SortOrder:   bookmark.SortOrder,
		Username:    user(r).Username,
	})
	if err != nil {
		render.Render(w, r, api.ErrInvalidRequest(err))
		return
	}
	render.Render(w, r, api.SuccessResult(http.StatusOK, fmt.Sprintf("bookmark item updated: %s/%s", bookmark.Path, bookmark.DisplayName)))
}

// --------------------------------------------------------------------------
// internal helpers
// --------------------------------------------------------------------------

func mapBookmark(item store.BookmarkItem) Bookmark {
	var t string
	switch item.Type {
	case store.Node:
		t = Node
	case store.Folder:
		t = Folder
	}
	return Bookmark{
		DisplayName: item.DisplayName,
		Path:        item.Path,
		NodeID:      item.ItemID,
		SortOrder:   item.SortOrder,
		URL:         item.URL,
		Type:        t,
		Modified:    item.Modified,
		Created:     item.Created,
		UserName:    item.Username,
	}
}

func mapBookmarks(vs []store.BookmarkItem) []Bookmark {
	vsm := make([]Bookmark, len(vs))
	for i, v := range vs {
		vsm[i] = mapBookmark(v)
	}
	return vsm
}

func user(r *http.Request) *security.User {
	user := r.Context().Value(core.ContextUser).(*security.User)
	if user == nil {
		panic("could not get User from context")
	}
	return user
}
