package webserver

import (
	"errors"
	"net/http"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/storage/delivery"
	"github.com/Activer007/SupaShare/internal/webserver/errorHandling"
)

type deliveryView struct {
	pageView
	IsAdminView    bool
	IsDownloadView bool
	PublicName     string
	CustomContent  customStatic
	Title          string
	Note           string
	Files          []delivery.PublicFile
}

func showDelivery(w http.ResponseWriter, r *http.Request) {
	addNoCacheHeader(w)
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d, err := delivery.GetPublic(r.Context(), r.PathValue("token"))
	if err != nil {
		if !errors.Is(err, delivery.ErrNotFound) {
			http.Error(w, "Delivery temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		view := &genericView{IsGenericError: true, ErrorId: errorHandling.TypeFileNotFound,
			PublicName: configuration.Get().PublicName, CustomContent: customStaticInfo}
		view.BaseHref = publicUploadBasePath()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		addPageResponsePolicy(w, "error")
		w.WriteHeader(http.StatusNotFound)
		helper.CheckIgnoreTimeout(renderPage(w, r, "error", view))
		return
	}
	config := configuration.Get()
	files, err := delivery.PublicFiles(r.Context(), d, config.ServerUrl, config.IncludeFilename)
	if err != nil {
		http.Error(w, "Delivery temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	view := &deliveryView{PublicName: config.PublicName, CustomContent: customStaticInfo, Title: d.Title, Note: d.Note, Files: files}
	view.BaseHref = publicUploadBasePath()
	helper.CheckIgnoreTimeout(renderPage(w, r, "delivery", view))
}
