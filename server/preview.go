package server

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/preview"
)

// Seeing the board.
//
// Everything else this package returns is a number in a table, and a number in
// a table cannot answer the question a layout engineer actually asks first:
// where is this trace, and what did you do to it. So the board is served as
// geometry the browser can draw -- a JSON description and a buffer of
// triangles, in the format the EMI Analyzer's viewer already reads.
//
// Two boards are on offer: the one that was uploaded, and the result of
// applying the plan. Flipping between them is how the tool shows its work,
// because a meander is obvious on screen and invisible in a diff.
//
// The geometry is built per request rather than cached. It takes about 150 ms
// for a 2.4 MB board, the result is a couple of megabytes, and the browser
// fetches it once per board -- so a cache would be state to get wrong for no
// gain the user could feel. Same reasoning as the rest of the package: no
// worker, no queue.

// handlePreviewBoard serves board.json for a session's board.
func (s *Service) handlePreviewBoard(w http.ResponseWriter, r *http.Request) {
	doc, _, ok := s.preview(w, r)
	if !ok {
		return
	}
	raw, err := doc.JSON()
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// handlePreviewGeometry serves geometry.bin for a session's board.
func (s *Service) handlePreviewGeometry(w http.ResponseWriter, r *http.Request) {
	_, bin, ok := s.preview(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(bin)
}

// preview builds the geometry for whichever board the request asked for,
// answering the request itself on failure.
//
// `of=result` is the tuned board, which only exists after an apply. `zones=1`
// draws the copper pours: they are most of the copper on a board by area, they
// sit under the traces this tool exists to change, and leaving them out halves
// the download -- so they are opt-in, and the document says when they are
// missing rather than letting a plane look like a hole.
func (s *Service) preview(w http.ResponseWriter, r *http.Request) (*preview.Doc, []byte, bool) {
	_, sess, ok := s.session(w, r)
	if !ok {
		return nil, nil, false
	}
	which := r.URL.Query().Get("of")
	if which != "" && which != "board" && which != "result" {
		s.fail(w, r, http.StatusBadRequest,
			fmt.Errorf("of=%q: expected \"board\" or \"result\"", which))
		return nil, nil, false
	}

	var b *board.Board
	if which == "result" {
		raw, err := s.deps.Store.Result(r.Context(), sess.ID)
		if err != nil {
			s.fail(w, r, http.StatusNotFound,
				errors.New("nothing has been applied to this board yet, so there is no result to look at"))
			return nil, nil, false
		}
		b, err = board.Parse(raw, resultName(sess.Filename))
		if err != nil {
			s.fail(w, r, http.StatusInternalServerError, fmt.Errorf("%w: %v", ErrBadBoard, err))
			return nil, nil, false
		}
	} else {
		var err error
		b, _, err = s.reload(r, sess)
		if err != nil {
			s.fail(w, r, http.StatusNotFound, err)
			return nil, nil, false
		}
	}

	name := sess.Filename
	if which == "result" {
		name = resultName(sess.Filename)
	}
	doc, bin, err := preview.Build(b, preview.Options{
		IncludeZones: r.URL.Query().Get("zones") == "1",
		Source: map[string]any{
			"filename": name,
			"session":  sess.ID,
			"of":       either(which, "board"),
		},
	})
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, err)
		return nil, nil, false
	}
	return doc, bin, true
}

func either(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
