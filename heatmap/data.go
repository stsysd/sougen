package heatmap

import (
	"time"
)

// Data holds the date and count for each day.
type Data struct {
	Date  time.Time
	Count int
}

// Options configures rendering parameters and value ranges.
type Options struct {
	CellSize    int      // size of each day cell (px)
	CellPadding int      // padding between cells (px)
	Colors      []string // array of N CSS colors for levels 0..N-1
	FontSize    int      // font size for month labels (px)
	FontFamily  string   // font family for labels
	ValueRanges []int    // optional thresholds for levels 1..N-1; len(ValueRanges)==len(Colors)-1
	ProjectName string   // project name for title
	Tags        []string // tags filter for title
}
