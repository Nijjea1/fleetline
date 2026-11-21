// Package bus wraps franz-go, a fast pure-Go Kafka client, with the few operations
// Fleetline needs: create topics, publish JSON with a key, consume JSON in a group.
// Redpanda locally and AWS MSK later both speak this protocol (D-004).
package bus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

// DefaultPartitions per topic. Partitions are the unit of parallelism: a consumer group
// can have at most this many active consumers per topic.
const DefaultPartitions = 12

// EnsureTopics creates any missing topics. Safe to call on every start.
func EnsureTopics(ctx context.Context, brokers []string, topics ...string) error {
	cl, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		return err
	}
	defer cl.Close()
	adm := kadm.NewClient(cl)
	resp, err := adm.CreateTopics(ctx, DefaultPartitions, 1, nil, topics...)
	if err != nil {
		return err
	}
	for _, r := range resp {
		if r.Err != nil && !errors.Is(r.Err, kerr.TopicAlreadyExists) {
			return fmt.Errorf("create topic %s: %w", r.Topic, r.Err)
		}
	}
	return nil
}

// Producer publishes JSON messages asynchronously.
type Producer struct {
	cl *kgo.Client
	// OnError is called for failed async produces (e.g. to count them in metrics).
	OnError func(error)
}

// NewProducer builds a producer tuned for throughput: records are batched for up to
// `linger` before being sent, trading a little latency for far fewer network round trips.
// E1 measures this trade-off.
func NewProducer(brokers []string, linger time.Duration) (*Producer, error) {
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ProducerLinger(linger),
		// Hash the key to choose the partition: same key -> same partition -> ordered.
		kgo.RecordPartitioner(kgo.StickyKeyPartitioner(nil)),
		// Bound in-memory buffering; Produce blocks when full. This is backpressure:
		// a slow broker slows the producer instead of growing memory without limit.
		kgo.MaxBufferedRecords(100_000),
	)
	if err != nil {
		return nil, err
	}
	return &Producer{cl: cl}, nil
}

// Publish JSON-encodes v and sends it to topic under key.
func (p *Producer) Publish(ctx context.Context, topic, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.cl.Produce(ctx, &kgo.Record{Topic: topic, Key: []byte(key), Value: b}, func(_ *kgo.Record, err error) {
		if err != nil && p.OnError != nil {
			p.OnError(err)
		}
	})
	return nil
}

// Flush waits until every buffered record is acknowledged.
func (p *Producer) Flush(ctx context.Context) error { return p.cl.Flush(ctx) }

// Close flushes and closes the client.
func (p *Producer) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = p.cl.Flush(ctx)
	p.cl.Close()
}

// Message is one consumed record with metadata useful for latency measurement.
type Message struct {
	Topic     string
	Key       string
	Value     []byte
	Partition int32
	Offset    int64
	// BrokerTime is when the broker appended the record (wall clock). now - BrokerTime
	// is the time the message spent waiting in Kafka: consumer lag in seconds.
	BrokerTime time.Time
}

// Consumer reads topics as part of a consumer group.
type Consumer struct{ cl *kgo.Client }

// NewConsumer joins `group`. With fromStart, a brand-new group reads from the oldest
// record; otherwise it starts at the newest (only messages produced after it starts).
func NewConsumer(brokers []string, group string, fromStart bool, topics ...string) (*Consumer, error) {
	reset := kgo.NewOffset().AtEnd()
	if fromStart {
		reset = kgo.NewOffset().AtStart()
	}
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(topics...),
		kgo.ConsumeResetOffset(reset),
		kgo.FetchMaxWait(50*time.Millisecond), // don't hold fetches long: we care about latency
	)
	if err != nil {
		return nil, err
	}
	return &Consumer{cl: cl}, nil
}

// Run polls until ctx is done, calling handle for every message. Offsets are committed
// automatically in the background (at-least-once delivery: after a crash a few messages
// may be seen twice, so handlers must be idempotent; the Redis seq/ts guard is).
func (c *Consumer) Run(ctx context.Context, handle func(Message)) error {
	for {
		fetches := c.cl.PollFetches(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			return fmt.Errorf("fetch %s[%d]: %w", errs[0].Topic, errs[0].Partition, errs[0].Err)
		}
		fetches.EachRecord(func(r *kgo.Record) {
			handle(Message{
				Topic: r.Topic, Key: string(r.Key), Value: r.Value,
				Partition: r.Partition, Offset: r.Offset, BrokerTime: r.Timestamp,
			})
		})
	}
}

// Close leaves the group and closes the client.
func (c *Consumer) Close() { c.cl.Close() }
