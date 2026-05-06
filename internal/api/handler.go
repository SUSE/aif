package api

import "net/http"

type Handler interface {
	Register(mux *http.ServeMux)
}
