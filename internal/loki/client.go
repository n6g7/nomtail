package loki

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/n6g7/nomtail/pkg/log"
	"github.com/n6g7/nomtail/pkg/version"
)

const (
	// Period between flush attempts
	flushPeriod time.Duration = 15 * time.Second
	// Loki push request timeout. Keep this lower than the flush period
	pushTimout time.Duration = 10 * time.Second
	// Final flush timeout: how long we give ourselves to send any remaining
	// cached value after the entries channel has closed.
	finalFlushTimout time.Duration = 10 * time.Second

	chanBufferSize int    = 32
	contentType    string = "application/x-protobuf"
)

var userAgent string = fmt.Sprintf("nomtail/%s", version.Display())

type client struct {
	logger     *log.Logger
	entries    chan Entry
	batch      *batch
	httpClient *http.Client
	url        string
}

func NewClient(logger *log.Logger, address string) *client {
	client := client{
		logger:     logger.With("component", "loki"),
		entries:    make(chan Entry, chanBufferSize),
		batch:      newBatch(),
		httpClient: http.DefaultClient,
		url:        address,
	}

	return &client
}

func (l *client) flush(ctx context.Context) {
	l.logger.DebugContext(ctx, "flushing...", "streams", len(l.batch.streams), "bytes", l.batch.bytes)

	err := l.pushBatch(ctx, l.batch)
	if err != nil {
		l.logger.WarnContext(ctx, "failed to send batch to Loki", "err", err)
	} else {
		l.batch.reset()
	}
}

func (l *client) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	flushTicker := time.Tick(flushPeriod)

	for {
		select {
		case entry, ok := <-l.entries:
			if !ok {
				l.logger.DebugContext(ctx, "entries channel closed, shutting down...")
				// Not re-using client ctx as it may have been cancelled already.
				finalCtx, cancel := context.WithTimeout(context.Background(), finalFlushTimout)
				defer cancel()
				l.flush(finalCtx)

				// What do we do if that last flush fails?

				l.logger.InfoContext(ctx, "bye")
				return
			}

			l.logger.TraceContext(ctx, "received entry", "entry", entry)
			l.batch.add(&entry)

		case <-flushTicker:
			if l.batch.readyToSend() {
				l.flush(ctx)
			}
		}
	}
}

func (l *client) Chan() chan<- Entry {
	return l.entries
}

func (l *client) pushBatch(ctx context.Context, b *batch) error {
	l.logger.DebugContext(ctx, "sending batch to Loki", "stream_count", len(b.streams))

	buf, err := b.makePushRequest()
	if err != nil {
		return fmt.Errorf("failed to generate Loki protobuf push request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, pushTimout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", l.url, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("failed to create Loki HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", userAgent)

	response, err := l.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("loki push request failed: %w", err)
	}

	if response.StatusCode/100 != 2 {
		scanner := bufio.NewScanner(response.Body)
		var line string
		if scanner.Scan() {
			line = scanner.Text()
		}
		return fmt.Errorf("Promtail returned status code %s (%d): %s", response.Status, response.StatusCode, line)
	}

	l.logger.DebugContext(ctx, "cache pushed to Loki successfully", "status_code", response.StatusCode)

	if err := response.Body.Close(); err != nil {
		return fmt.Errorf("failed to close response body: %w", err)
	}

	return nil
}
