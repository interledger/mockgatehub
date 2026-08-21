package handler

import "net/http"

// apiError couples a failure with the HTTP status it should be reported as.
//
// It exists so the core operations — simulating a card transaction, settling a
// withdrawal — can be called both from an HTTP endpoint and from the admin UI
// without either caller owning the status codes, and without the two growing
// their own diverging copies of the same rules.
type apiError struct {
	status int
	msg    string
	// details carries extra fields to include in a JSON error body, such as
	// the list of valid scenario keys.
	details map[string]interface{}
}

func (e *apiError) Error() string { return e.msg }

func badRequest(msg string) *apiError { return &apiError{status: http.StatusBadRequest, msg: msg} }
func notFound(msg string) *apiError   { return &apiError{status: http.StatusNotFound, msg: msg} }
func conflict(msg string) *apiError   { return &apiError{status: http.StatusConflict, msg: msg} }
func internalErr(msg string) *apiError {
	return &apiError{status: http.StatusInternalServerError, msg: msg}
}

func (e *apiError) withDetails(details map[string]interface{}) *apiError {
	e.details = details
	return e
}

// sendAPIError writes an error to an HTTP response, preferring the status the
// operation chose and falling back to 500 for anything unclassified.
func (h *Handler) sendAPIError(w http.ResponseWriter, err error) {
	apiErr, ok := err.(*apiError)
	if !ok {
		h.sendError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if len(apiErr.details) == 0 {
		h.sendError(w, apiErr.status, apiErr.msg)
		return
	}

	body := map[string]interface{}{
		"error":   http.StatusText(apiErr.status),
		"message": apiErr.msg,
	}
	for k, v := range apiErr.details {
		body[k] = v
	}
	h.sendJSON(w, apiErr.status, body)
}

// errFailedToUpdateUser is returned when a user record could not be persisted.
var errFailedToUpdateUser = internalErr("failed to update user")
