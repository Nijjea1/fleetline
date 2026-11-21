//go:build integration

// Run with: make up && go test -tags integration ./internal/bus/
package bus

import (
	"context"
	"fmt"
	"testing"
	"time"
)

var brokers = []string{"localhost:19092"}

func TestRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	topic := fmt.Sprintf("test.roundtrip.%d", time.Now().UnixNano())
	if err := EnsureTopics(ctx, brokers, topic); err != nil {
		t.Fatal(err)
	}
	p, err := NewProducer(brokers, 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	const n = 1000
	for i := 0; i < n; i++ {
		if err := p.Publish(ctx, topic, fmt.Sprintf("k%d", i%7), map[string]int{"i": i}); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	c, err := NewConsumer(brokers, topic+".group", true, topic)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	got := 0
	lastOffset := map[int32]int64{}
	runCtx, stop := context.WithCancel(ctx)
	_ = c.Run(runCtx, func(m Message) {
		if prev, ok := lastOffset[m.Partition]; ok && m.Offset <= prev {
			t.Errorf("offsets not increasing in partition %d", m.Partition)
		}
		lastOffset[m.Partition] = m.Offset
		if got++; got == n {
			stop()
		}
	})
	if got != n {
		t.Fatalf("consumed %d, want %d", got, n)
	}
}
