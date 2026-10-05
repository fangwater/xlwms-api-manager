package httpapi

import "net/http"

func (s *Server) listFulfillmentWarehouses(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.FulfillmentWarehouses(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, response{Success: false, Error: "cannot load fulfillment warehouses"})
		return
	}
	writeJSON(w, http.StatusOK, response{Success: true, Data: items})
}
