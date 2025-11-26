// Package model provides value objects for API parameter validation.
package model

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// HexID represents an ID that is serialized as a 16-digit zero-padded hex string.
// The zero value (valid=false) represents an uninitialized or invalid ID.
type HexID struct {
	value int64
	valid bool
}

// NewHexID creates a valid HexID from an int64 value.
func NewHexID(id int64) HexID {
	return HexID{value: id, valid: true}
}

// ToInt64 returns the internal int64 value.
// Returns 0 if the HexID is invalid.
func (h HexID) ToInt64() int64 {
	if !h.valid {
		return 0
	}
	return h.value
}

// IsValid returns true if the HexID has been properly initialized.
func (h HexID) IsValid() bool {
	return h.valid
}

// Equals returns true if two HexIDs are equal.
func (h HexID) Equals(other HexID) bool {
	if !h.valid && !other.valid {
		return true // Both invalid
	}
	if h.valid != other.valid {
		return false // One valid, one invalid
	}
	return h.value == other.value
}

// MarshalJSON converts the HexID to a 16-digit zero-padded hex string for JSON.
// Invalid HexIDs are marshaled as null.
func (h HexID) MarshalJSON() ([]byte, error) {
	if !h.valid {
		return []byte("null"), nil
	}
	hexStr := fmt.Sprintf("%016x", h.value)
	return json.Marshal(hexStr)
}

// UnmarshalJSON parses a hex string from JSON and converts it to HexID.
func (h *HexID) UnmarshalJSON(data []byte) error {
	// Handle null
	if string(data) == "null" {
		h.valid = false
		h.value = 0
		return nil
	}

	var hexStr string
	if err := json.Unmarshal(data, &hexStr); err != nil {
		return fmt.Errorf("hex id must be a string: %w", err)
	}

	id, err := strconv.ParseInt(hexStr, 16, 64)
	if err != nil {
		return fmt.Errorf("invalid hex id format: %w", err)
	}

	h.value = id
	h.valid = true
	return nil
}

// ProjectName represents a project name value object.
type ProjectName struct {
	value string
}

// NewProjectName creates a new project name value object.
func NewProjectName(name string) (*ProjectName, error) {
	if name == "" {
		return nil, fmt.Errorf("project name is required")
	}
	return &ProjectName{value: name}, nil
}

// String returns the project name string.
func (p *ProjectName) String() string {
	return p.value
}

// DateRange represents a date range value object.
type DateRange struct {
	from time.Time
	to   time.Time
}

// NewDateRange creates a new date range value object.
func NewDateRange(fromStr, toStr string) (*DateRange, error) {
	var fromTime, toTime time.Time
	var err error

	// Process from parameter
	if fromStr != "" {
		fromTime, err = parseDateTime(fromStr)
		if err != nil {
			return nil, fmt.Errorf("invalid from parameter. Use ISO8601 format (YYYY-MM-DD or YYYY-MM-DDThh:mm:ssZ)")
		}
	} else {
		// Set default value
		defaultFrom, _ := getDefaultDateRange()
		fromTime = defaultFrom
	}

	// Process to parameter
	if toStr != "" {
		toTime, err = parseDateTime(toStr)
		if err != nil {
			return nil, fmt.Errorf("invalid to parameter. Use ISO8601 format (YYYY-MM-DD or YYYY-MM-DDThh:mm:ssZ)")
		}
	} else {
		// Set default value
		_, defaultTo := getDefaultDateRange()
		toTime = defaultTo
	}

	// Normalize from time to beginning of day (00:00:00)
	fromTime = normalizeToBeginOfDay(fromTime)
	// Normalize to time to end of day (23:59:59.999999999)
	toTime = normalizeToEndOfDay(toTime)

	return &DateRange{from: fromTime, to: toTime}, nil
}

// From returns the start date.
func (d *DateRange) From() time.Time {
	return d.from
}

// To returns the end date.
func (d *DateRange) To() time.Time {
	return d.to
}

// getDefaultDateRange calculates the default date range for the latest week + 52 weeks.
func getDefaultDateRange() (time.Time, time.Time) {
	now := time.Now()
	weekday := int(now.Weekday())
	latestWeekStart := now.AddDate(0, 0, -weekday)
	defaultFrom := latestWeekStart.AddDate(0, 0, -52*7)
	return defaultFrom, now
}

// normalizeToBeginOfDay normalizes time to beginning of day (00:00:00).
func normalizeToBeginOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// normalizeToEndOfDay normalizes time to end of day (23:59:59.999999999).
func normalizeToEndOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 23, 59, 59, 999999999, t.Location())
}

// parseDateTime parses date string with flexible format support.
func parseDateTime(dateStr string) (time.Time, error) {
	// Try RFC3339 format first (with time)
	if t, err := time.Parse(time.RFC3339, dateStr); err == nil {
		return t, nil
	}

	// Try date-only format (YYYY-MM-DD)
	if t, err := time.Parse("2006-01-02", dateStr); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("unable to parse date")
}

// Tags represents a tags list value object.
type Tags struct {
	values []string
}

// NewTags creates a new tags value object.
func NewTags(tagsStr string) *Tags {
	if tagsStr == "" {
		return &Tags{values: nil}
	}

	// Split by comma
	tags := strings.Split(tagsStr, ",")
	// Trim whitespace
	for i, tag := range tags {
		tags[i] = strings.TrimSpace(tag)
	}
	// Remove empty tags
	var filteredTags []string
	for _, tag := range tags {
		if tag != "" {
			filteredTags = append(filteredTags, tag)
		}
	}

	return &Tags{values: filteredTags}
}

// Values returns the tag list.
func (t *Tags) Values() []string {
	return t.values
}

// IsEmpty checks if the tags are empty.
func (t *Tags) IsEmpty() bool {
	return len(t.values) == 0
}

// Timestamp represents a timestamp value object.
type Timestamp struct {
	value time.Time
}

// NewTimestamp creates a new timestamp value object.
func NewTimestamp(timestampStr string) (*Timestamp, error) {
	if timestampStr == "" {
		// Use current time for empty string
		return &Timestamp{value: time.Now()}, nil
	}

	timestamp, err := time.Parse(time.RFC3339, timestampStr)
	if err != nil {
		return nil, fmt.Errorf("invalid datetime format. Use ISO8601 format (YYYY-MM-DDThh:mm:ssZ)")
	}

	return &Timestamp{value: timestamp}, nil
}

// Time returns the time value.
func (t *Timestamp) Time() time.Time {
	return t.value
}

// Value represents a positive integer value object.
type Value struct {
	value int
}

// NewValue creates a new value object.
func NewValue(val *int) (*Value, error) {
	if val == nil {
		// Use default value 1 for nil
		return &Value{value: 1}, nil
	}

	if *val < 1 {
		return nil, fmt.Errorf("value must be a positive integer greater than 0")
	}

	return &Value{value: *val}, nil
}

// Int returns the integer value.
func (v *Value) Int() int {
	return v.value
}

// RecordFilterParams represents filter parameters for record queries.
type RecordFilterParams struct {
	ProjectID HexID    `json:"project_id"`     // Project ID for filtering
	From      string   `json:"from"`           // Start date for filtering (RFC3339)
	To        string   `json:"to"`             // End date for filtering (RFC3339)
	Tags      []string `json:"tags,omitempty"` // Tags for filtering
}

// RecordCursor represents a keyset cursor for record pagination.
// It embeds RecordFilterParams to guarantee all filter parameters are included.
type RecordCursor struct {
	RecordFilterParams        // Embedded filter parameters
	Timestamp          string `json:"timestamp"` // RFC3339 formatted timestamp of the last record
	ID                 HexID  `json:"id"`        // ID of the last record
}

// ProjectCursor represents a keyset cursor for project pagination.
type ProjectCursor struct {
	UpdatedAt string `json:"updated_at"` // RFC3339 formatted updated_at of the last project
	Name      string `json:"name"`       // Name of the last project
}

// EncodeRecordCursor encodes a record cursor to a Base64 string.
func EncodeRecordCursor(timestamp time.Time, id HexID, projectID HexID, from, to time.Time, tags []string) string {
	// Convert zero-value times to empty strings
	fromStr := ""
	if !from.IsZero() {
		fromStr = from.Format(time.RFC3339)
	}
	toStr := ""
	if !to.IsZero() {
		toStr = to.Format(time.RFC3339)
	}

	cursor := RecordCursor{
		RecordFilterParams: RecordFilterParams{
			ProjectID: projectID,
			From:      fromStr,
			To:        toStr,
			Tags:      tags,
		},
		Timestamp: timestamp.Format(time.RFC3339),
		ID:        id,
	}
	jsonData, _ := json.Marshal(cursor)
	return base64.URLEncoding.EncodeToString(jsonData)
}

// DecodeRecordCursor decodes a Base64 encoded record cursor string.
func DecodeRecordCursor(encoded string) (*RecordCursor, error) {
	if encoded == "" {
		return nil, nil
	}

	decoded, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor: failed to decode base64: %w", err)
	}

	var cursor RecordCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return nil, fmt.Errorf("invalid cursor: failed to unmarshal json: %w", err)
	}

	return &cursor, nil
}

// EncodeProjectCursor encodes a project cursor to a Base64 string.
func EncodeProjectCursor(updatedAt time.Time, name string) string {
	cursor := ProjectCursor{
		UpdatedAt: updatedAt.Format(time.RFC3339),
		Name:      name,
	}
	jsonData, _ := json.Marshal(cursor)
	return base64.URLEncoding.EncodeToString(jsonData)
}

// DecodeProjectCursor decodes a Base64 encoded project cursor string.
func DecodeProjectCursor(encoded string) (*ProjectCursor, error) {
	if encoded == "" {
		return nil, nil
	}

	decoded, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor: failed to decode base64: %w", err)
	}

	var cursor ProjectCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return nil, fmt.Errorf("invalid cursor: failed to unmarshal json: %w", err)
	}

	return &cursor, nil
}

// Pagination represents cursor-based pagination parameters for records and projects.
type Pagination struct {
	limit  int
	cursor *string // Cursor for pagination (nil means start from the beginning)
}

// NewPagination creates a new cursor-based pagination value object.
func NewPagination(limitStr, cursorStr string) (*Pagination, error) {
	limit := 100 // Default value

	// Process limit parameter
	if limitStr != "" {
		parsedLimit, err := parseInt(limitStr)
		if err != nil {
			return nil, fmt.Errorf("invalid limit parameter: must be a positive integer")
		}
		if parsedLimit <= 0 {
			return nil, fmt.Errorf("limit must be greater than 0")
		}
		if parsedLimit > 1000 { // Set upper limit
			parsedLimit = 1000
		}
		limit = parsedLimit
	}

	// Process cursor parameter
	var cursor *string
	if cursorStr != "" {
		cursor = &cursorStr
	}

	return &Pagination{limit: limit, cursor: cursor}, nil
}

// NewPaginationWithValues creates a Pagination directly from values (for internal use).
// No validation is performed on the values.
func NewPaginationWithValues(limit int, cursor *string) *Pagination {
	return &Pagination{limit: limit, cursor: cursor}
}

// Limit returns the limit value.
func (p *Pagination) Limit() int {
	return p.limit
}

// Cursor returns the cursor string for cursor-based pagination.
// Returns nil if no cursor is set (i.e., start from the beginning).
func (p *Pagination) Cursor() *string {
	return p.cursor
}

// parseInt converts a string to an integer and handles errors.
func parseInt(s string) (int, error) {
	var value int
	var err error
	if _, err = fmt.Sscanf(s, "%d", &value); err != nil {
		return 0, err
	}
	return value, nil
}
