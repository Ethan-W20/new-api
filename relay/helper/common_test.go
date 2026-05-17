package helper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingFlusher struct {
	count atomic.Int64
}

func (f *countingFlusher) Flush() {
	f.count.Add(1)
}

func TestStreamFlushBatch_FlushesEveryMaxFrames(t *testing.T) {
	flusher := &countingFlusher{}
	batch := &streamFlushBatch{flusher: flusher}

	for i := 0; i < streamFlushMaxFrames-1; i++ {
		require.NoError(t, batch.markFrame())
	}
	assert.Equal(t, int64(0), flusher.count.Load())

	require.NoError(t, batch.markFrame())
	assert.Equal(t, int64(1), flusher.count.Load())
}

func TestStreamFlushBatch_FlushesAfterDelay(t *testing.T) {
	flusher := &countingFlusher{}
	batch := &streamFlushBatch{flusher: flusher}

	require.NoError(t, batch.markFrame())

	require.Eventually(t, func() bool {
		return flusher.count.Load() == 1
	}, 200*time.Millisecond, time.Millisecond)
}

func TestStreamFlushBatch_ForceFlushCancelsTimer(t *testing.T) {
	flusher := &countingFlusher{}
	batch := &streamFlushBatch{flusher: flusher}

	require.NoError(t, batch.markFrame())
	require.NoError(t, batch.forceFlush())

	assert.Equal(t, int64(1), flusher.count.Load())
	time.Sleep(streamFlushDelay * 3)
	assert.Equal(t, int64(1), flusher.count.Load())
}

func TestDoneForceFlushesPendingDoneFrame(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	Done(c)

	assert.True(t, recorder.Flushed)
	assert.Contains(t, recorder.Body.String(), "data: [DONE]")
}

func TestPingDataForceFlushesImmediately(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	require.NoError(t, PingData(c))

	assert.True(t, recorder.Flushed)
	assert.Contains(t, recorder.Body.String(), ": PING")
}

func TestForceFlushCancelledRequestStopsTimerWithoutFlush(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)

	require.NoError(t, StringData(c, `{"id":1}`))
	cancel()
	err := ForceFlush(c)

	require.Error(t, err)
	assert.False(t, recorder.Flushed)
	time.Sleep(streamFlushDelay * 3)
	assert.False(t, recorder.Flushed)
}

func TestStreamFlushBatch_ConcurrentFirstUseSharesBatch(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	const writers = 20
	start := make(chan struct{})
	errCh := make(chan error, writers)

	var wg sync.WaitGroup
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func() {
			defer wg.Done()
			<-start
			errCh <- StringData(c, `{"id":1}`)
		}()
	}

	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}
	require.NoError(t, ForceFlush(c))

	assert.True(t, recorder.Flushed)
	assert.Equal(t, writers, strings.Count(recorder.Body.String(), "data: "))
}
