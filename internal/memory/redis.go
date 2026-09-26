/*
Copyright 2024 Nick Merrett.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nickmerrett/agent-operator/internal/llm"
)

// redisStore implements Store using Redis.
type redisStore struct {
	client           *redis.Client
	workingMemoryTTL time.Duration
	episodicTTL      time.Duration
}

// key helpers
func workingMemoryKey(agentRunName string) string {
	return fmt.Sprintf("agentrun:%s:messages", agentRunName)
}

func resultKey(agentRunName string) string {
	return fmt.Sprintf("agentrun:%s:result", agentRunName)
}

func inboundChannel(agentRunName string) string {
	return fmt.Sprintf("agentrun:%s:inbound", agentRunName)
}

func episodesKey(agentName string) string {
	return fmt.Sprintf("agent:%s:episodes", agentName)
}

func (s *redisStore) AppendMessage(ctx context.Context, agentRunName string, msg llm.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("memory: marshal message: %w", err)
	}
	key := workingMemoryKey(agentRunName)
	pipe := s.client.Pipeline()
	pipe.RPush(ctx, key, string(data))
	pipe.Expire(ctx, key, s.workingMemoryTTL)
	_, err = pipe.Exec(ctx)
	return err
}

func (s *redisStore) GetMessages(ctx context.Context, agentRunName string) ([]llm.Message, error) {
	vals, err := s.client.LRange(ctx, workingMemoryKey(agentRunName), 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("memory: get messages: %w", err)
	}
	msgs := make([]llm.Message, 0, len(vals))
	for _, v := range vals {
		var m llm.Message
		if err := json.Unmarshal([]byte(v), &m); err != nil {
			return nil, fmt.Errorf("memory: unmarshal message: %w", err)
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func (s *redisStore) SetResult(ctx context.Context, agentRunName string, result Result) error {
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("memory: marshal result: %w", err)
	}
	key := resultKey(agentRunName)
	pipe := s.client.Pipeline()
	pipe.Set(ctx, key, string(data), s.workingMemoryTTL)
	_, err = pipe.Exec(ctx)
	return err
}

func (s *redisStore) GetResult(ctx context.Context, agentRunName string) (*Result, error) {
	val, err := s.client.Get(ctx, resultKey(agentRunName)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("memory: get result: %w", err)
	}
	var r Result
	if err := json.Unmarshal([]byte(val), &r); err != nil {
		return nil, fmt.Errorf("memory: unmarshal result: %w", err)
	}
	return &r, nil
}

func (s *redisStore) PublishInbound(ctx context.Context, agentRunName string, message string) error {
	return s.client.Publish(ctx, inboundChannel(agentRunName), message).Err()
}

func (s *redisStore) SubscribeInbound(ctx context.Context, agentRunName string) (<-chan string, func(), error) {
	sub := s.client.Subscribe(ctx, inboundChannel(agentRunName))
	ch := make(chan string, 16)

	go func() {
		defer close(ch)
		for msg := range sub.Channel() {
			select {
			case ch <- msg.Payload:
			case <-ctx.Done():
				return
			}
		}
	}()

	cancel := func() {
		_ = sub.Close()
	}
	return ch, cancel, nil
}

func (s *redisStore) AppendEpisode(ctx context.Context, agentName string, summary string, ts time.Time) error {
	key := episodesKey(agentName)
	z := redis.Z{
		Score:  float64(ts.Unix()),
		Member: summary,
	}
	pipe := s.client.Pipeline()
	pipe.ZAdd(ctx, key, z)
	pipe.Expire(ctx, key, s.episodicTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *redisStore) GetEpisodes(ctx context.Context, agentName string, limit int) ([]Episode, error) {
	// Return the N most recent (highest score).
	vals, err := s.client.ZRevRangeWithScores(ctx, episodesKey(agentName), 0, int64(limit-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("memory: get episodes: %w", err)
	}
	eps := make([]Episode, 0, len(vals))
	for _, z := range vals {
		eps = append(eps, Episode{
			Summary:   fmt.Sprintf("%v", z.Member),
			Timestamp: time.Unix(int64(z.Score), 0),
		})
	}
	return eps, nil
}

func (s *redisStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

// NewRedisStore creates a Redis-backed memory Store.
func NewRedisStore(client *redis.Client, workingTTL, episodicTTL time.Duration) Store {
	return &redisStore{
		client:           client,
		workingMemoryTTL: workingTTL,
		episodicTTL:      episodicTTL,
	}
}
