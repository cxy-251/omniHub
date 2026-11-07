package server

import (
	"encoding/json"
	"net/http"
	"omnihub/internal/rubiks"
)

type rubiksSolveRequest struct {
	Dimension int    `json:"dimension"`
	State     string `json:"state"`
}

type rubiksSolveResponse struct {
	OK        bool     `json:"ok"`
	Dimension int      `json:"dimension"`
	Moves     []string `json:"moves"`
	Method    string   `json:"method,omitempty"`
	Count     int      `json:"count"`
	Error     string   `json:"error,omitempty"`
}

func (r *Router) handleWebLabRubiksSolve(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !r.allowPath(w, req, "weblab/rubiks") {
		return
	}

	var body rubiksSolveRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, rubiksSolveResponse{
			OK:    false,
			Error: "invalid json payload: " + err.Error(),
		})
		return
	}

	var (
		moves  []string
		method string
		err    error
	)
	switch body.Dimension {
	case 4:
		moves, method, err = rubiks.Solve4x4(body.State)
	case 5:
		moves, method, err = rubiks.Solve5x5(body.State)
	case 6:
		moves, method, err = rubiks.Solve6x6(body.State)
	default:
		writeJSON(w, http.StatusBadRequest, rubiksSolveResponse{
			OK:    false,
			Error: "backend supports dimensions 4, 5, and 6",
		})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, rubiksSolveResponse{
			OK:        false,
			Dimension: body.Dimension,
			Error:     err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, rubiksSolveResponse{
		OK:        true,
		Dimension: body.Dimension,
		Moves:     moves,
		Method:    method,
		Count:     len(moves),
	})
}
