package model

import "strings"

type Level int8

const (
	LevelTrace Level = iota
	LevelDebug
	LevelInfo
	LevelNotice
	LevelWarning
	LevelError
	LevelCritical
)

var levelNames = [...]string{"trace", "debug", "info", "notice", "warning", "error", "critical"}

func (l Level) String() string {
	if int(l) < 0 || int(l) >= len(levelNames) {
		return "unknown"
	}
	return levelNames[l]
}

// MarshalJSON encodes Level as its lowercase string name.
func (l Level) MarshalJSON() ([]byte, error) {
	return []byte(`"` + l.String() + `"`), nil
}

// UnmarshalJSON accepts either a string name or numeric value.
func (l *Level) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if v, ok := ParseLevel(s); ok {
		*l = v
		return nil
	}
	// numeric fallback
	for i, n := range levelNames {
		if s == n {
			*l = Level(i)
			return nil
		}
	}
	return &ParseError{Value: s}
}

func ParseLevel(s string) (Level, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for i, n := range levelNames {
		if n == s {
			return Level(i), true
		}
	}
	return 0, false
}

type ParseError struct{ Value string }

func (e *ParseError) Error() string { return "invalid level: " + e.Value }
