package openai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushes atomic.Int64
}

func (r *flushRecorder) Flush() {
	r.flushes.Add(1)
	r.ResponseRecorder.Flush()
}

func newFinalResponseTestContext() (*gin.Context, *flushRecorder) {
	recorder := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c, recorder
}

func marshalFinalStreamChunk(t *testing.T, finishReason string, usage *dto.Usage) string {
	t.Helper()

	chunk := dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl-test",
		Object:  "chat.completion.chunk",
		Created: 123,
		Model:   "test-model",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Index:        0,
				FinishReason: &finishReason,
			},
		},
		Usage: usage,
	}
	data, err := json.Marshal(chunk)
	require.NoError(t, err)
	return string(data)
}

func TestHandleFinalResponse_ForceFlushesGeminiTerminalFrame(t *testing.T) {
	c, recorder := newFinalResponseTestContext()
	usage := &dto.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatGemini}

	HandleFinalResponse(c, info, marshalFinalStreamChunk(t, "stop", usage),
		"chatcmpl-test", 123, "test-model", "", usage, true)

	assert.Positive(t, recorder.flushes.Load())
	assert.Contains(t, recorder.Body.String(), "data: ")
}

func TestHandleFinalResponse_ForceFlushesClaudeTerminalFrame(t *testing.T) {
	c, recorder := newFinalResponseTestContext()
	usage := &dto.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}
	info := &relaycommon.RelayInfo{
		RelayFormat:       types.RelayFormatClaude,
		SendResponseCount: 1,
		ClaudeConvertInfo: &relaycommon.ClaudeConvertInfo{
			LastMessagesType: relaycommon.LastMessageTypeNone,
		},
	}

	HandleFinalResponse(c, info, marshalFinalStreamChunk(t, "stop", usage),
		"chatcmpl-test", 123, "test-model", "", usage, true)

	assert.Positive(t, recorder.flushes.Load())
	assert.Contains(t, recorder.Body.String(), "message_stop")
}
