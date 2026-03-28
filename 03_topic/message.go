package main

import (
	"time"

	"github.com/google/uuid"
)

// BenchMessage is the payload generated and published to YDB topics.
type BenchMessage struct {
	ID     uuid.UUID `json:"id"`
	UserID uuid.UUID `json:"user_id"`
	Type   string    `json:"type"`
}

// ScenarioResult holds metrics collected for one benchmark scenario.
type ScenarioResult struct {
	Name      string
	Messages  int64
	TLIErrors int64
	Duration  time.Duration
	MsgPerSec float64
}

// ProducerResult holds metrics collected for one publish run.
type ProducerResult struct {
	Name      string
	Messages  int64
	Duration  time.Duration
	MsgPerSec float64
}
