// yearly_heatmap.go
// Generates a GitHub-like yearly contribution heatmap as an SVG string in Go.
package heatmap

import (
	"fmt"
	"strings"
	"time"
)

// GenerateYearlyHeatmapSVG returns an SVG string representing the yearly heatmap.
// data should be sorted in ascending order by date.
func GenerateYearlyHeatmapSVG(data []Data, opts *Options) string {
	// default options
	if opts == nil {
		opts = &Options{
			CellSize:    12,
			CellPadding: 2,
			FontSize:    10,
			FontFamily:  "sans-serif",
			Colors:      []string{"#f0f0f0", "#c6e48b", "#7bc96f", "#239a3b", "#196127", "#0d4429"},
		}
	}

	if len(data) == 0 {
		return ""
	}

	// determine date range from data (assuming data is in ascending order)
	startDate := data[0].Date
	endDate := data[len(data)-1].Date

	// map date string to count
	countMap := make(map[string]int, len(data))
	for _, d := range data {
		key := d.Date.Format("2006-01-02")
		countMap[key] = d.Count
	}

	// align first column to Sunday
	firstSunday := startDate
	weekday := int(startDate.Weekday())
	firstSunday = firstSunday.AddDate(0, 0, -weekday)

	// calculate required number of weeks
	dayDiff := endDate.Sub(firstSunday).Hours() / 24
	weeks := int(dayDiff/7) + 1 // add 1 to ensure we have enough columns

	// compute dimensions
	titleHeight := 0
	if opts.ProjectName != "" || len(opts.Tags) > 0 {
		titleHeight = opts.FontSize + 8 // title text + padding
	}
	width := weeks*(opts.CellSize+opts.CellPadding) + opts.CellPadding
	height := 7*(opts.CellSize+opts.CellPadding) + opts.CellPadding + opts.FontSize + 4 + titleHeight

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`<svg width="%d" height="%d" xmlns="http://www.w3.org/2000/svg">`+"\n", width, height))
	sb.WriteString(fmt.Sprintf(`  <style>.label{font-family:%s;font-size:%dpx;fill:#666}.title{font-family:%s;font-size:%dpx;fill:#333;font-weight:bold}</style>`+"\n",
		opts.FontFamily, opts.FontSize, opts.FontFamily, opts.FontSize))

	// render title if project name or tags are provided
	if opts.ProjectName != "" || len(opts.Tags) > 0 {
		titleY := opts.FontSize
		title := ""
		if opts.ProjectName != "" {
			title = opts.ProjectName
		}
		if len(opts.Tags) > 0 {
			tagsStr := strings.Join(opts.Tags, ", ")
			if title != "" {
				title += " (tags: " + tagsStr + ")"
			} else {
				title = "tags: " + tagsStr
			}
		}
		sb.WriteString(fmt.Sprintf(`  <text x="%d" y="%d" class="title">%s</text>`+"\n",
			opts.CellPadding, titleY, title))
	}

	// month labels
	months := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	lastMonth := -1
	oneDay := 24 * time.Hour
	monthLabelY := opts.FontSize + titleHeight
	for w := range weeks {
		x := opts.CellPadding + w*(opts.CellSize+opts.CellPadding)
		current := firstSunday.Add(time.Duration(w*7) * oneDay)
		if current.Day() <= 7 && int(current.Month())-1 != lastMonth {
			sb.WriteString(fmt.Sprintf(`  <text x="%d" y="%d" class="label">%s</text>`+"\n",
				x, monthLabelY, months[current.Month()-1]))
			lastMonth = int(current.Month()) - 1
		}
	}

	// find the maximum count for auto-scaling
	supCount := 5
	for _, d := range data {
		if d.Count+1 > supCount {
			supCount = d.Count + 1
		}
	}

	// draw cells with 0 value special handling
	levels := len(opts.Colors)
	for w := range weeks {
		for i := range 7 {
			current := firstSunday.Add(time.Duration(w*7+i) * oneDay)
			key := current.Format("2006-01-02")
			count, exists := countMap[key]
			if !exists {
				continue
			}
			level := 0
			
			// 0値の場合は常にレベル0（薄いグレー）を使用
			if count == 0 {
				level = 0
			} else if supCount > 1 {
				// 1以上の値を1からlevels-1の範囲に分散
				level = ((count - 1) * (levels - 2)) / (supCount - 1) + 1
				if level >= levels {
					level = levels - 1
				}
				if level < 1 {
					level = 1
				}
			} else {
				level = 1
			}
			x := opts.CellPadding + w*(opts.CellSize+opts.CellPadding)
			y := opts.CellPadding + opts.FontSize + 4 + titleHeight + i*(opts.CellSize+opts.CellPadding)

			// 各セルに矩形と、その中にtitle要素（ツールチップ）を追加
			sb.WriteString(fmt.Sprintf(`  <rect x="%d" y="%d" width="%d" height="%d" fill="%s" data-date="%s" data-count="%d">`+"\n",
				x, y, opts.CellSize, opts.CellSize, opts.Colors[level], key, count))

			// 日付をフォーマットして表示用の文字列を作成
			displayDate := current.Format("2006年01月02日")
			sb.WriteString(fmt.Sprintf(`    <title>%s: %d</title>`+"\n", displayDate, count))
			sb.WriteString(`  </rect>` + "\n")
		}
	}

	sb.WriteString(`</svg>`)
	return sb.String()
}

