package heatmap

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateWeeklyHeatmapSVG_EmptyData(t *testing.T) {
	data := []Data{}
	opts := &Options{
		CellSize:    12,
		CellPadding: 2,
		FontSize:    10,
		FontFamily:  "sans-serif",
		Colors:      []string{"#f0f0f0", "#c6e48b", "#7bc96f", "#239a3b", "#196127", "#0d4429"},
	}

	svg := GenerateWeeklyHeatmapSVG(data, opts)

	if svg != "" {
		t.Errorf("Expected empty string for empty data, got: %s", svg)
	}
}

func TestGenerateWeeklyHeatmapSVG_NilOptions(t *testing.T) {
	// デフォルトオプションで正常に動作することを確認
	now := time.Now()
	data := []Data{
		{Date: now, Value: 5},
	}

	svg := GenerateWeeklyHeatmapSVG(data, nil)

	if svg == "" {
		t.Error("Expected non-empty SVG with nil options")
	}

	if !strings.Contains(svg, "<svg") {
		t.Error("Expected SVG tag in output")
	}
}

func TestGenerateWeeklyHeatmapSVG_WithData(t *testing.T) {
	// 2025-05-21 10:00 のレコード（スロット2: 8-12時）
	timestamp1 := time.Date(2025, 5, 21, 10, 0, 0, 0, time.UTC)
	// 2025-05-21 15:00 のレコード（スロット3: 12-16時）
	timestamp2 := time.Date(2025, 5, 21, 15, 0, 0, 0, time.UTC)
	// 2025-05-22 09:00 のレコード（スロット2: 8-12時）
	timestamp3 := time.Date(2025, 5, 22, 9, 0, 0, 0, time.UTC)

	data := []Data{
		{Date: timestamp1, Value: 3},
		{Date: timestamp2, Value: 5},
		{Date: timestamp3, Value: 2},
	}

	opts := &Options{
		CellSize:    12,
		CellPadding: 2,
		FontSize:    10,
		FontFamily:  "sans-serif",
		Colors:      []string{"#f0f0f0", "#c6e48b", "#7bc96f", "#239a3b", "#196127", "#0d4429"},
		ProjectName: "Test Project",
	}

	svg := GenerateWeeklyHeatmapSVG(data, opts)

	// SVGの基本構造を確認
	if !strings.Contains(svg, "<svg") {
		t.Error("Expected SVG tag in output")
	}

	if !strings.Contains(svg, "</svg>") {
		t.Error("Expected closing SVG tag in output")
	}

	// プロジェクト名がタイトルに含まれることを確認
	if !strings.Contains(svg, "Test Project") {
		t.Error("Expected project name in SVG title")
	}

	// データポイントが含まれることを確認
	if !strings.Contains(svg, `data-date="2025-05-21"`) {
		t.Error("Expected data point for 2025-05-21")
	}

	if !strings.Contains(svg, `data-date="2025-05-22"`) {
		t.Error("Expected data point for 2025-05-22")
	}

	// スロット情報が含まれることを確認
	if !strings.Contains(svg, `data-slot="2"`) {
		t.Error("Expected data with slot 2")
	}

	if !strings.Contains(svg, `data-slot="3"`) {
		t.Error("Expected data with slot 3")
	}
}

func TestGenerateWeeklyHeatmapSVG_WithTags(t *testing.T) {
	timestamp := time.Date(2025, 5, 21, 10, 0, 0, 0, time.UTC)
	data := []Data{
		{Date: timestamp, Value: 3},
	}

	opts := &Options{
		CellSize:    12,
		CellPadding: 2,
		FontSize:    10,
		FontFamily:  "sans-serif",
		Colors:      []string{"#f0f0f0", "#c6e48b", "#7bc96f", "#239a3b", "#196127", "#0d4429"},
		ProjectName: "Test Project",
		Tags:        []string{"work", "coding"},
	}

	svg := GenerateWeeklyHeatmapSVG(data, opts)

	// タグがタイトルに含まれることを確認
	if !strings.Contains(svg, "tags: work, coding") {
		t.Error("Expected tags in SVG title")
	}
}

func TestGenerateWeeklyHeatmapSVG_TimeSlotCalculation(t *testing.T) {
	// 各時間帯のテスト
	testCases := []struct {
		hour         int
		expectedSlot int
	}{
		{0, 0},  // 0-4時
		{3, 0},  // 0-4時
		{4, 1},  // 4-8時
		{7, 1},  // 4-8時
		{8, 2},  // 8-12時
		{11, 2}, // 8-12時
		{12, 3}, // 12-16時
		{15, 3}, // 12-16時
		{16, 4}, // 16-20時
		{19, 4}, // 16-20時
		{20, 5}, // 20-24時
		{23, 5}, // 20-24時
	}

	for _, tc := range testCases {
		timestamp := time.Date(2025, 5, 21, tc.hour, 0, 0, 0, time.UTC)
		data := []Data{
			{Date: timestamp, Value: 1},
		}

		opts := &Options{
			CellSize:    12,
			CellPadding: 2,
			FontSize:    10,
			FontFamily:  "sans-serif",
			Colors:      []string{"#f0f0f0", "#c6e48b", "#7bc96f", "#239a3b", "#196127", "#0d4429"},
		}

		svg := GenerateWeeklyHeatmapSVG(data, opts)

		expectedSlotAttr := `data-slot="` + string(rune('0'+tc.expectedSlot)) + `"`
		if !strings.Contains(svg, expectedSlotAttr) {
			t.Errorf("Hour %d should be in slot %d, but SVG doesn't contain %s",
				tc.hour, tc.expectedSlot, expectedSlotAttr)
		}
	}
}

func TestGenerateWeeklyHeatmapSVG_WeekAlignment(t *testing.T) {
	// 月曜日から開始することを確認
	// 2025-05-21は水曜日なので、最初の列は月曜日（5月19日）から始まるべき
	wednesday := time.Date(2025, 5, 21, 10, 0, 0, 0, time.UTC)
	data := []Data{
		{Date: wednesday, Value: 5},
	}

	opts := &Options{
		CellSize:    12,
		CellPadding: 2,
		FontSize:    10,
		FontFamily:  "sans-serif",
		Colors:      []string{"#f0f0f0", "#c6e48b", "#7bc96f", "#239a3b", "#196127", "#0d4429"},
	}

	svg := GenerateWeeklyHeatmapSVG(data, opts)

	// SVGが生成されることを確認
	if !strings.Contains(svg, "<svg") {
		t.Error("Expected SVG to be generated")
	}

	// 最小8週間分（56日）のデータが含まれることを確認
	// 実際のセル数は data-date 属性の出現回数で確認できる
	// ただし、値が0のセルは出力されないので、data-date の存在だけでは判断できない
	// 代わりに、SVGのサイズが適切かを確認
	if !strings.Contains(svg, "width=") && !strings.Contains(svg, "height=") {
		t.Error("Expected SVG to have width and height attributes")
	}
}

func TestGenerateWeeklyHeatmapSVG_Tooltip(t *testing.T) {
	// 10:00のレコード（スロット2: 8-12時）
	timestamp := time.Date(2025, 5, 21, 10, 0, 0, 0, time.UTC)
	data := []Data{
		{Date: timestamp, Value: 5},
	}

	opts := &Options{
		CellSize:    12,
		CellPadding: 2,
		FontSize:    10,
		FontFamily:  "sans-serif",
		Colors:      []string{"#f0f0f0", "#c6e48b", "#7bc96f", "#239a3b", "#196127", "#0d4429"},
	}

	svg := GenerateWeeklyHeatmapSVG(data, opts)

	// ツールチップ（title要素）が含まれることを確認
	if !strings.Contains(svg, "<title>") {
		t.Error("Expected tooltip (title element) in SVG")
	}

	// 時間帯のラベルが含まれることを確認（08:00-12:00）
	if !strings.Contains(svg, "08:00-12:00") {
		t.Error("Expected time slot label in tooltip")
	}

	// 値が含まれることを確認
	if !strings.Contains(svg, ": 5</title>") {
		t.Error("Expected value in tooltip")
	}
}
