package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newResponsesCompactionTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	return c, recorder
}

func TestOaiResponsesCompactionHandlerForwardsRecoveredNonEmptyBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"id":"cmp-1","object":"response.compaction","created_at":1775555723,"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"recovered summary"}]}],"usage":{"input_tokens":8,"output_tokens":28,"total_tokens":36}}`
	c, recorder := newResponsesCompactionTestContext()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, err := OaiResponsesCompactionHandler(c, resp)
	if err != nil {
		t.Fatalf("OaiResponsesCompactionHandler error: %v", err)
	}
	if usage == nil || usage.TotalTokens != 36 {
		t.Fatalf("usage = %#v, want total tokens 36", usage)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if got := recorder.Body.String(); got != body {
		t.Fatalf("body = %s, want %s", got, body)
	}
}

func TestOaiResponsesCompactionHandlerPreservesCompactFailureAsError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"error":{"message":"codex compact returned empty output after retry: empty output","type":"upstream_error","code":"bad_gateway"}}`
	c, recorder := newResponsesCompactionTestContext()
	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, err := OaiResponsesCompactionHandler(c, resp)
	if err == nil {
		t.Fatal("OaiResponsesCompactionHandler error = nil, want compact failure propagation")
	}
	if usage != nil {
		t.Fatalf("usage = %#v, want nil on propagated compact failure", usage)
	}
	if err.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", err.StatusCode, http.StatusBadGateway)
	}
	if !strings.Contains(err.Error(), "codex compact returned empty output after retry") {
		t.Fatalf("error = %q, want compact retry exhaustion", err.Error())
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("handler wrote success body on compact failure: %s", recorder.Body.String())
	}
}
