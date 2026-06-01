package handler

import (
	"io/fs"
	"net/http"

	"github.com/ReGHZ/arghzprint/ui"
)

func (h *Handler) UI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeFileFS(w, r, ui.FS, "index.html")
}

func (h *Handler) Assets() http.Handler {
	// sub-FS rooted at assets/ so StripPrefix("/assets/", ...) resolves correctly
	sub, err := fs.Sub(ui.FS, "assets")
	if err != nil {
		panic("ui assets sub-fs: " + err.Error())
	}
	return http.StripPrefix("/assets/", http.FileServerFS(sub))
}
