package loki

import (
	"fmt"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/grafana/loki/pkg/push"
)

const (
	maxStreamAge time.Duration = 2 * time.Second
	minBytes     int           = 4 * 1024
)

type batch struct {
	streams map[string]*push.Stream

	oldestStreamTime *time.Time
	bytes            int
}

func newBatch() *batch {
	return &batch{
		streams:          make(map[string]*push.Stream),
		oldestStreamTime: nil,
		bytes:            0,
	}
}

func (b *batch) readyToSend() bool {
	// Never send empty batches
	if len(b.streams) == 0 {
		return false
	}

	// Always send batches with old streams
	if time.Since(*b.oldestStreamTime) > maxStreamAge {
		return true
	}

	// Send when batch reaches a certain size.
	return b.bytes >= minBytes
}

func (b *batch) add(entry *Entry) {
	labelsString := entry.Labels.String()

	if b.oldestStreamTime == nil {
		now := time.Now()
		b.oldestStreamTime = &now
	}
	b.bytes += len(entry.Line)
	if stream, ok := b.streams[labelsString]; ok {
		stream.Entries = append(stream.Entries, entry.Entry)
	}
	b.streams[labelsString] = &push.Stream{
		Labels: labelsString,
		Entries: []push.Entry{
			entry.Entry,
		},
	}
}

func (b *batch) reset() {
	b.oldestStreamTime = nil
	b.streams = make(map[string]*push.Stream)
	b.bytes = 0
}

func (b *batch) makePushRequest() ([]byte, error) {
	// Prepare PushRequest
	streams := make([]push.Stream, 0)
	for _, stream := range b.streams {
		streams = append(streams, *stream)
	}
	request := push.PushRequest{
		Streams: streams,
	}

	// Marshall and compress
	buf, err := proto.Marshal(&request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshall PushRequest protobuf: %w", err)
	}
	buf = snappy.Encode(nil, buf)

	return buf, nil
}
