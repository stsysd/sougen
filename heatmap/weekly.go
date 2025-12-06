package heatmap

import (
	"fmt"
	"strings"
	"time"
)

// GenerateWeeklyHeatmapSVG generates an SVG heatmap with hourly granularity
// Layout: 6 rows (4-hour slots) x N days (multiple weeks)
// Each row represents a 4-hour time slot (0-4, 4-8, 8-12, 12-16, 16-20, 20-24)
func GenerateWeeklyHeatmapSVG(data []Data, opts *Options) string {
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

	// determine date range from data (assuming data is in ascending order by date)
	startDate := data[0].Date
	endDate := data[len(data)-1].Date

	// map date+hour to value
	// key format: "2006-01-02-slot" where slot is 0-5
	valueMap := make(map[string]int, len(data))
	for _, d := range data {
		hour := d.Date.Hour()
		slot := hour / 4 // 0-5 for 6 time slots
		key := fmt.Sprintf("%s-%d", d.Date.Format("2006-01-02"), slot)
		valueMap[key] += d.Value
	}

	// align first column to Monday
	firstMonday := startDate
	weekday := int(startDate.Weekday())
	// convert Sunday (0) to 7 for calculation
	if weekday == 0 {
		weekday = 7
	}
	firstMonday = firstMonday.AddDate(0, 0, -(weekday - 1))

	// calculate required number of days
	dayDiff := int(endDate.Sub(firstMonday).Hours()/24) + 1
	days := dayDiff
	if days < 56 { // minimum 8 weeks
		days = 56
	}

	// compute dimensions
	titleHeight := 0
	if opts.ProjectName != "" || len(opts.Tags) > 0 {
		titleHeight = opts.FontSize + 8 // title text + padding
	}

	// calculate width considering extra spacing between weeks
	weeks := (days + 6) / 7
	weekSpacing := opts.CellPadding * 2 // extra spacing between Sunday and Monday
	width := days*(opts.CellSize+opts.CellPadding) + opts.CellPadding + (weeks-1)*weekSpacing
	height := 6*(opts.CellSize+opts.CellPadding) + opts.CellPadding + opts.FontSize + 4 + titleHeight

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

	// date labels for each week (Monday only)
	dateLabelY := opts.FontSize + titleHeight
	oneDay := 24 * time.Hour

	// find the maximum value for auto-scaling
	supValue := 5
	for _, d := range data {
		if d.Value+1 > supValue {
			supValue = d.Value + 1
		}
	}

	levels := len(opts.Colors)

	// draw cells
	for d := 0; d < days; d++ {
		current := firstMonday.Add(time.Duration(d) * oneDay)
		currentWeekday := int(current.Weekday())
		if currentWeekday == 0 {
			currentWeekday = 7
		}

		// calculate x position with extra spacing after Sunday
		weekNum := d / 7
		extraSpacing := weekNum * weekSpacing
		x := opts.CellPadding + d*(opts.CellSize+opts.CellPadding) + extraSpacing

		// show date label for Monday (weekday == 1)
		if currentWeekday == 1 {
			dateLabel := current.Format("01/02")
			sb.WriteString(fmt.Sprintf(`  <text x="%d" y="%d" class="label">%s</text>`+"\n",
				x, dateLabelY, dateLabel))
		}

		// draw 6 time slot cells for this day
		for slot := 0; slot < 6; slot++ {
			dateKey := current.Format("2006-01-02")
			key := fmt.Sprintf("%s-%d", dateKey, slot)
			value, exists := valueMap[key]
			if !exists {
				continue
			}

			level := 0
			// 0値の場合は常にレベル0（薄いグレー）を使用
			if value == 0 {
				level = 0
			} else if supValue > 1 {
				// 1以上の値を1からlevels-1の範囲に分散
				level = ((value-1)*(levels-2))/(supValue-1) + 1
				if level >= levels {
					level = levels - 1
				}
				if level < 1 {
					level = 1
				}
			} else {
				level = 1
			}

			y := opts.CellPadding + opts.FontSize + 4 + titleHeight + slot*(opts.CellSize+opts.CellPadding)

			// 各セルに矩形と、その中にtitle要素（ツールチップ）を追加
			sb.WriteString(fmt.Sprintf(`  <rect x="%d" y="%d" width="%d" height="%d" fill="%s" data-date="%s" data-slot="%d" data-value="%d">`+"\n",
				x, y, opts.CellSize, opts.CellSize, opts.Colors[level], dateKey, slot, value))

			// 日付と時間帯をフォーマットして表示用の文字列を作成
			displayDate := current.Format("2006年01月02日")
			timeSlotLabel := fmt.Sprintf("%02d:00-%02d:00", slot*4, (slot+1)*4)
			sb.WriteString(fmt.Sprintf(`    <title>%s %s: %d</title>`+"\n", displayDate, timeSlotLabel, value))
			sb.WriteString(`  </rect>` + "\n")
		}
	}

	sb.WriteString(`</svg>`)
	return sb.String()
}
