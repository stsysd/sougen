// Package model provides value objects for API parameter validation.
package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

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

// RecordID represents a record ID value object.
type RecordID struct {
	value uuid.UUID
}

// NewRecordID creates a new record ID value object.
func NewRecordID(idStr string) (*RecordID, error) {
	if idStr == "" {
		return nil, fmt.Errorf("record ID is required")
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid UUID format")
	}

	return &RecordID{value: id}, nil
}

// UUID returns the UUID value.
func (r *RecordID) UUID() uuid.UUID {
	return r.value
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

// Pagination represents pagination parameters value object.
type Pagination struct {
	limit  int
	offset int
}

// NewPagination creates a new pagination value object.
func NewPagination(limitStr, offsetStr string) (*Pagination, error) {
	limit := 100 // Default value
	offset := 0  // Default value

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

	// Process offset parameter
	if offsetStr != "" {
		parsedOffset, err := parseInt(offsetStr)
		if err != nil {
			return nil, fmt.Errorf("invalid offset parameter: must be a non-negative integer")
		}
		if parsedOffset < 0 {
			return nil, fmt.Errorf("offset must be non-negative")
		}
		offset = parsedOffset
	}

	return &Pagination{limit: limit, offset: offset}, nil
}

// Limit returns the limit value.
func (p *Pagination) Limit() int {
	return p.limit
}

// Offset returns the offset value.
func (p *Pagination) Offset() int {
	return p.offset
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