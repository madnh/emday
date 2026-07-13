// Package model defines the shared data types flowing through the pipeline:
// samples (metrics) from sources, and events heading to notifiers.
package model

import (
	"strconv"
	"time"
)

// Value is a metric value: a number or a string. Numbers support ordering
// operators in rule conditions; strings support equality/contains/etc.
type Value struct {
	Num   float64 `json:"num,omitempty"`
	Str   string  `json:"str,omitempty"`
	IsNum bool    `json:"is_num"`
}

// ParseValue interprets raw text as a number when possible, string otherwise.
func ParseValue(raw string) Value {
	if n, err := strconv.ParseFloat(raw, 64); err == nil {
		return Value{Num: n, IsNum: true}
	}
	return Value{Str: raw}
}

func NumValue(n float64) Value { return Value{Num: n, IsNum: true} }
func StrValue(s string) Value  { return Value{Str: s} }
func BoolValue(b bool) Value {
	if b {
		return NumValue(1)
	}
	return NumValue(0)
}

// Native returns the value as the type handed to expression evaluation.
func (v Value) Native() any {
	if v.IsNum {
		return v.Num
	}
	return v.Str
}

func (v Value) String() string {
	if v.IsNum {
		return strconv.FormatFloat(v.Num, 'f', -1, 64)
	}
	return v.Str
}

func (v Value) Equal(o Value) bool {
	return v.IsNum == o.IsNum && v.Num == o.Num && v.Str == o.Str
}

// Sample is one observed metric value.
type Sample struct {
	Metric string // e.g. "cpu.percent", "backup-status.BACKUP_STATUS"
	Value  Value
	Time   time.Time
}

type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Event is a notification heading to notifiers.
type Event struct {
	Source   string            `json:"source"` // "exec/backup-status", "rule/cpu-high"
	Level    Level             `json:"level"`
	Title    string            `json:"title"`
	Message  string            `json:"message,omitempty"`
	Time     time.Time         `json:"time"`
	Resolved bool              `json:"resolved,omitempty"`
	Fields   map[string]string `json:"fields,omitempty"`
}
