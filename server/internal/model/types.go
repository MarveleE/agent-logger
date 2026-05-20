package model

// Session is created by the client at app launch and groups a stream of logs.
type Session struct {
	ID         string            `json:"id"`
	BundleID   string            `json:"bundleId"`             // logical "app product id"; iOS bundle id, Android applicationId, web user-supplied name, etc.
	DeviceID   string            `json:"deviceId"`             // stable per machine/instance
	DeviceName string            `json:"deviceName,omitempty"` // human-readable label
	DeviceKind string            `json:"deviceKind,omitempty"` // open vocabulary: simulator | emulator | device | browser | desktop | server | container | embedded
	Platform   string            `json:"platform,omitempty"`   // ios | macos | android | web | node | windows | linux | …; free-form
	OSVersion  string            `json:"osVersion,omitempty"`
	AppVersion string            `json:"appVersion,omitempty"` // semver-style (e.g. CFBundleShortVersionString)
	AppBuild   string            `json:"appBuild,omitempty"`   // build number (e.g. CFBundleVersion, Android versionCode, git short SHA)
	StartedAt  int64             `json:"startedAt"`            // unix milliseconds
	LastSeenAt int64             `json:"lastSeenAt"`           // unix milliseconds
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// LogEntry is a single log line emitted by the client.
type LogEntry struct {
	ID        int64             `json:"id,omitempty"` // server-assigned, omitted on ingest
	SessionID string            `json:"sessionId,omitempty"`
	Seq       int64             `json:"seq"`
	Timestamp int64             `json:"ts"` // unix milliseconds
	Level     Level             `json:"level"`
	Category  string            `json:"category,omitempty"`
	Message   string            `json:"message"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	File      string            `json:"file,omitempty"`
	Line      int               `json:"line,omitempty"`
	Function  string            `json:"func,omitempty"`
}

// LogBatch is the wire format for POST /v1/sessions/{id}/logs.
type LogBatch struct {
	Batch []LogEntry `json:"batch"`
}
