package heatmap

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateYearlyHeatmapSVG_NoFutureDates(t *testing.T) {
	// 2025-01-01から2025-01-15までのデータを生成
	// 2025-01-15は水曜日で、その週の土曜日は2025-01-18
	// endDateを超えた日付（2025-01-16, 01-17, 01-18）が含まれないことを確認

	data := []Data{
		{Date: time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), Value: 1},
		{Date: time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), Value: 2},
		{Date: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), Value: 3},
	}

	opts := &Options{
		CellSize:    12,
		CellPadding: 2,
		FontSize:    10,
		FontFamily:  "sans-serif",
		Colors:      []string{"#f0f0f0", "#c6e48b", "#7bc96f", "#239a3b", "#196127", "#0d4429"},
		From:        time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:          time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
	}

	svg := GenerateYearlyHeatmapSVG(data, opts)

	// SVGが生成されることを確認
	if !strings.Contains(svg, "<svg") {
		t.Error("Expected SVG to be generated")
	}

	// endDate（2025-01-15）は含まれるべき
	if !strings.Contains(svg, `data-date="2025-01-15"`) {
		t.Error("Expected endDate (2025-01-15) to be included")
	}

	// endDateを超えた日付（2025-01-16, 01-17, 01-18）は含まれないべき
	if strings.Contains(svg, `data-date="2025-01-16"`) {
		t.Error("Future date 2025-01-16 should not be included")
	}
	if strings.Contains(svg, `data-date="2025-01-17"`) {
		t.Error("Future date 2025-01-17 should not be included")
	}
	if strings.Contains(svg, `data-date="2025-01-18"`) {
		t.Error("Future date 2025-01-18 should not be included")
	}
}

func TestGenerateYearlyHeatmapSVG_EndDateOnSunday(t *testing.T) {
	// 2025-01-05は日曜日
	// この場合、その週の土曜日（2025-01-04）までで終わるべき

	data := []Data{
		{Date: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Value: 1},
		{Date: time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), Value: 2},
	}

	opts := &Options{
		CellSize:    12,
		CellPadding: 2,
		FontSize:    10,
		FontFamily:  "sans-serif",
		Colors:      []string{"#f0f0f0", "#c6e48b", "#7bc96f", "#239a3b", "#196127", "#0d4429"},
		From:        time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:          time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC),
	}

	svg := GenerateYearlyHeatmapSVG(data, opts)

	// endDate（2025-01-05 日曜日）は含まれるべき
	if !strings.Contains(svg, `data-date="2025-01-05"`) {
		t.Error("Expected endDate (2025-01-05) to be included")
	}

	// その次の日（2025-01-06 月曜日）は含まれないべき
	if strings.Contains(svg, `data-date="2025-01-06"`) {
		t.Error("Future date 2025-01-06 should not be included")
	}
}
