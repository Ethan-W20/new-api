package helper

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	streamFlushDelay       = 10 * time.Millisecond
	streamFlushMaxFrames   = 10
	streamFlushBatchCtxKey = "stream_flush_batch_state"
)

var streamFlushBatchCreateMu sync.Mutex

type streamFlushBatch struct {
	mu      sync.Mutex
	flusher http.Flusher
	timer   *time.Timer
	pending int
	closed  atomic.Bool
}

func getStreamFlushBatch(c *gin.Context, flusher http.Flusher) *streamFlushBatch {
	if c == nil {
		return nil
	}
	if existing, ok := c.Get(streamFlushBatchCtxKey); ok {
		if batch, ok := existing.(*streamFlushBatch); ok && batch != nil {
			batch.mu.Lock()
			batch.flusher = flusher
			batch.mu.Unlock()
			return batch
		}
	}

	streamFlushBatchCreateMu.Lock()
	defer streamFlushBatchCreateMu.Unlock()

	if existing, ok := c.Get(streamFlushBatchCtxKey); ok {
		if batch, ok := existing.(*streamFlushBatch); ok && batch != nil {
			batch.mu.Lock()
			batch.flusher = flusher
			batch.mu.Unlock()
			return batch
		}
	}

	batch := &streamFlushBatch{flusher: flusher}
	c.Set(streamFlushBatchCtxKey, batch)
	return batch
}

func existingStreamFlushBatch(c *gin.Context) *streamFlushBatch {
	if c == nil {
		return nil
	}
	if existing, ok := c.Get(streamFlushBatchCtxKey); ok {
		if batch, ok := existing.(*streamFlushBatch); ok {
			return batch
		}
	}
	return nil
}

func discardStreamFlushBatch(c *gin.Context) {
	if batch := existingStreamFlushBatch(c); batch != nil {
		batch.closeAndDiscard()
	}
}

func writeStreamFrame(c *gin.Context, write func() error) error {
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}

	if c.Request != nil && c.Request.Context().Err() != nil {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return errors.New("streaming error: flusher not found")
	}

	return getStreamFlushBatch(c, flusher).writeFrame(write)
}

func (b *streamFlushBatch) writeFrame(write func() error) error {
	if b == nil {
		if write != nil {
			return write()
		}
		return nil
	}
	if b.closed.Load() {
		return errors.New("stream flush batch closed")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed.Load() {
		return errors.New("stream flush batch closed")
	}

	if write != nil {
		if err := write(); err != nil {
			return err
		}
	}

	return b.markFrameLocked()
}

func (b *streamFlushBatch) markFrame() error {
	if b == nil {
		return nil
	}
	if b.closed.Load() {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	return b.markFrameLocked()
}

func (b *streamFlushBatch) markFrameLocked() error {
	if b.closed.Load() {
		return nil
	}
	b.pending++
	if b.pending >= streamFlushMaxFrames {
		return b.flushPendingLocked()
	} else if b.pending == 1 && b.timer == nil {
		b.timer = time.AfterFunc(streamFlushDelay, b.flushDue)
	}
	return nil
}

func (b *streamFlushBatch) flushDue() {
	if b == nil {
		return
	}
	if b.closed.Load() {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed.Load() {
		b.pending = 0
		b.stopTimerLocked()
		return
	}

	_ = b.flushPendingLocked()
}

func (b *streamFlushBatch) forceFlush() error {
	if b == nil {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.pending = 0
	b.stopTimerLocked()

	return b.flushLocked()
}

func (b *streamFlushBatch) discardPending() {
	if b == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.pending = 0
	b.stopTimerLocked()
}

func (b *streamFlushBatch) closeAndDiscard() {
	if b == nil {
		return
	}
	b.closed.Store(true)
	if !b.mu.TryLock() {
		return
	}
	defer b.mu.Unlock()
	b.pending = 0
	b.stopTimerLocked()
}

func (b *streamFlushBatch) flushPendingLocked() error {
	if b.pending == 0 {
		b.stopTimerLocked()
		return nil
	}
	b.pending = 0
	b.stopTimerLocked()
	return b.flushLocked()
}

func (b *streamFlushBatch) flushLocked() (err error) {
	if b.flusher == nil {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("flush panic recovered: %v", r)
		}
	}()
	b.flusher.Flush()
	return nil
}

func (b *streamFlushBatch) stopTimerLocked() {
	if b.timer == nil {
		return
	}
	b.timer.Stop()
	b.timer = nil
}

func FlushWriter(c *gin.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("flush panic recovered: %v", r)
		}
	}()

	if c == nil || c.Writer == nil {
		return nil
	}

	if c.Request != nil && c.Request.Context().Err() != nil {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return errors.New("streaming error: flusher not found")
	}

	return getStreamFlushBatch(c, flusher).markFrame()
}

// ForceFlush immediately flushes all pending data regardless of batching state.
func ForceFlush(c *gin.Context) error {
	if c == nil || c.Writer == nil {
		return nil
	}
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return errors.New("streaming error: flusher not found")
	}
	batch := getStreamFlushBatch(c, flusher)
	if c.Request != nil && c.Request.Context().Err() != nil {
		batch.closeAndDiscard()
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}
	return batch.forceFlush()
}

func SetEventStreamHeaders(c *gin.Context) {
	// 检查是否已经设置过头部
	if _, exists := c.Get("event_stream_headers_set"); exists {
		return
	}

	// 设置标志，表示头部已经设置过
	c.Set("event_stream_headers_set", true)

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
}

func ClaudeData(c *gin.Context, resp dto.ClaudeResponse) error {
	jsonData, err := common.Marshal(resp)
	if err != nil {
		common.SysError("error marshalling stream response: " + err.Error())
		return nil
	}
	return writeStreamFrame(c, func() error {
		c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("event: %s\n", resp.Type)})
		c.Render(-1, common.CustomEvent{Data: "data: " + string(jsonData)})
		return nil
	})
}

func ClaudeChunkData(c *gin.Context, resp dto.ClaudeResponse, data string) {
	_ = writeStreamFrame(c, func() error {
		c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("event: %s\n", resp.Type)})
		c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("data: %s\n", data)})
		return nil
	})
}

func ResponseChunkData(c *gin.Context, resp dto.ResponsesStreamResponse, data string) {
	_ = writeStreamFrame(c, func() error {
		c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("event: %s\n", resp.Type)})
		c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("data: %s", data)})
		return nil
	})
}

func StringData(c *gin.Context, str string) error {
	return writeStreamFrame(c, func() error {
		c.Render(-1, common.CustomEvent{Data: "data: " + str})
		return nil
	})
}

func PingData(c *gin.Context) error {
	if err := writeStreamFrame(c, func() error {
		if _, err := c.Writer.Write([]byte(": PING\n\n")); err != nil {
			return fmt.Errorf("write ping data failed: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	return ForceFlush(c)
}

func ObjectData(c *gin.Context, object interface{}) error {
	if object == nil {
		return errors.New("object is nil")
	}
	jsonData, err := common.Marshal(object)
	if err != nil {
		return fmt.Errorf("error marshalling object: %w", err)
	}
	return StringData(c, string(jsonData))
}

func Done(c *gin.Context) {
	_ = StringData(c, "[DONE]")
	_ = ForceFlush(c)
}

func WssString(c *gin.Context, ws *websocket.Conn, str string) error {
	if ws == nil {
		logger.LogError(c, "websocket connection is nil")
		return errors.New("websocket connection is nil")
	}
	//common.LogInfo(c, fmt.Sprintf("sending message: %s", str))
	return ws.WriteMessage(1, []byte(str))
}

func WssObject(c *gin.Context, ws *websocket.Conn, object interface{}) error {
	jsonData, err := common.Marshal(object)
	if err != nil {
		return fmt.Errorf("error marshalling object: %w", err)
	}
	if ws == nil {
		logger.LogError(c, "websocket connection is nil")
		return errors.New("websocket connection is nil")
	}
	//common.LogInfo(c, fmt.Sprintf("sending message: %s", jsonData))
	return ws.WriteMessage(1, jsonData)
}

func WssError(c *gin.Context, ws *websocket.Conn, openaiError types.OpenAIError) {
	if ws == nil {
		return
	}
	errorObj := &dto.RealtimeEvent{
		Type:    "error",
		EventId: GetLocalRealtimeID(c),
		Error:   &openaiError,
	}
	_ = WssObject(c, ws, errorObj)
}

func GetResponseID(c *gin.Context) string {
	logID := c.GetString(common.RequestIdKey)
	return fmt.Sprintf("chatcmpl-%s", logID)
}

func GetLocalRealtimeID(c *gin.Context) string {
	logID := c.GetString(common.RequestIdKey)
	return fmt.Sprintf("evt_%s", logID)
}

func GenerateStartEmptyResponse(id string, createAt int64, model string, systemFingerprint *string) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: systemFingerprint,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Role:    "assistant",
					Content: common.GetPointer(""),
				},
			},
		},
	}
}

func GenerateStopResponse(id string, createAt int64, model string, finishReason string) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: nil,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				FinishReason: &finishReason,
			},
		},
	}
}

func GenerateFinalUsageResponse(id string, createAt int64, model string, usage dto.Usage) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: nil,
		Choices:           make([]dto.ChatCompletionsStreamResponseChoice, 0),
		Usage:             &usage,
	}
}
