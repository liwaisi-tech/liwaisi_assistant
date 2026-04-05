package httpapi

import (
	"net/http"
	"regexp"
	"strings"
)

// emailRe is a basic email format validator.
var emailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// WaitlistRequest is the request body for POST /api/v1/waitlist.
type WaitlistRequest struct {
	Email string `json:"email"`
}

// WaitlistResponse is the response for POST /api/v1/waitlist.
type WaitlistOKResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// HandleWaitlist handles POST /api/v1/waitlist.
// Public endpoint — no authentication required.
func (h *Handlers) HandleWaitlist(w http.ResponseWriter, r *http.Request) {
	if h.WaitlistRepo == nil {
		writeError(w, http.StatusServiceUnavailable, "waitlist not available")
		return
	}

	var req WaitlistRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || !emailRe.MatchString(email) {
		writeError(w, http.StatusBadRequest, "invalid email format")
		return
	}

	if err := h.WaitlistRepo.Add(r.Context(), email, "landing_page"); err != nil {
		h.Logger.Error("waitlist add", "email", email, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, WaitlistOKResponse{
		OK:      true,
		Message: "You're on the list! We'll reach out soon.",
	})
}
