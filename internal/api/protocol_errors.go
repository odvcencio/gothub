package api

import (
	"bytes"
	"net/http"
	"strings"
)

// wrapGotBatchGraphErrors sanitizes object-graph traversal failures from the
// Got batch endpoint so protocol clients receive a stable structured error.
func (s *Server) wrapGotBatchGraphErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/objects/batch") {
			next.ServeHTTP(w, r)
			return
		}

		rec := newProtocolErrorCaptureWriter(w)
		next.ServeHTTP(rec, r)

		if !rec.wroteHeader {
			rec.WriteHeader(http.StatusOK)
		}
		if !rec.errorMode {
			return
		}

		status := rec.status
		headers := rec.header.Clone()
		body := rec.errorBody.Bytes()

		if isGotBatchGraphError(body) {
			status = http.StatusUnprocessableEntity
			headers = make(http.Header)
			headers.Set("Content-Type", "application/json")
			body = []byte(`{"error":"invalid object graph"}`)
		}

		writeCapturedProtocolError(w, status, headers, body)
	})
}

func isGotBatchGraphError(body []byte) bool {
	msg := strings.TrimSpace(string(body))
	return strings.HasPrefix(msg, "walk objects for ") || strings.HasPrefix(msg, "read object ")
}

type protocolErrorCaptureWriter struct {
	w           http.ResponseWriter
	header      http.Header
	status      int
	wroteHeader bool
	errorMode   bool
	errorBody   bytes.Buffer
}

func newProtocolErrorCaptureWriter(w http.ResponseWriter) *protocolErrorCaptureWriter {
	return &protocolErrorCaptureWriter{
		w:      w,
		header: make(http.Header),
	}
}

func (w *protocolErrorCaptureWriter) Header() http.Header {
	if w.wroteHeader && !w.errorMode {
		return w.w.Header()
	}
	return w.header
}

func (w *protocolErrorCaptureWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
	if status >= http.StatusBadRequest {
		w.errorMode = true
		return
	}
	copyHeaderValues(w.w.Header(), w.header)
	w.w.WriteHeader(status)
}

func (w *protocolErrorCaptureWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.errorMode {
		return w.errorBody.Write(p)
	}
	return w.w.Write(p)
}

func writeCapturedProtocolError(w http.ResponseWriter, status int, headers http.Header, body []byte) {
	dst := w.Header()
	for k := range dst {
		delete(dst, k)
	}
	copyHeaderValues(dst, headers)
	w.WriteHeader(status)
	if len(body) == 0 {
		return
	}
	_, _ = w.Write(body)
}

func copyHeaderValues(dst, src http.Header) {
	for k, vals := range src {
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}
