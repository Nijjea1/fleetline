# 01 · Repo, tooling and local infrastructure

## What we built
- A **monorepo**: one Git repo, one folder per layer (`sim/`, `core/`, `matcher/`, ...).
- `infra/docker-compose.yml` running **Redpanda** (Kafka-compatible broker), **Redpanda Console** (web UI for topics) and **Redis**.
- A `Makefile` so every common action is one command (`make up`, `make down`).

## Concepts

**Monorepo vs. many repos.** One repo means one commit can change the Go service *and* the dashboard that reads it, and a benchmark always points at one exact version of everything. Google and Uber both run large monorepos.

**Kafka in one paragraph.** Kafka is a durable, append-only log. Producers write messages to a *topic*; the topic is split into *partitions*; each message has a *key*, and the same key always lands in the same partition. Within a partition, order is guaranteed. Consumers read at their own pace and remember an *offset*. That decoupling is why Kafka sits between "pings arrive" and "dispatcher reads them": if the dispatcher is slow, messages wait in the log instead of being dropped (this is *backpressure*, which E1 measures).

**Why key by H3 cell.** All pings from one area go to one partition, so one consumer sees a consistent, ordered view of that neighborhood. It is how you'd shard dispatch geographically in production.

**Redpanda vs. Kafka.** Same wire protocol, single binary, no JVM/ZooKeeper. Our Go client cannot tell the difference, so AWS MSK (real Kafka) later is a config change.

**Redis.** In-memory key-value store. Vehicle state (position, status) changes every second and must be read in microseconds by the dispatcher; Redis is built for that. We disable persistence because state is rebuilt from the stream.

## Interview questions this answers
- "What happens if your consumer falls behind?" → Kafka buffers; lag grows; we measure lag in E1.
- "How would you scale dispatch to more cities?" → partition by geography (H3), one consumer group per region.
