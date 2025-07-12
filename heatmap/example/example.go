// Package main demonstrates the use of the heatmap package to generate SVG heatmaps.
package main

import (
	"fmt"
	"math/rand"

	// "math/rand"
	"time"

	"github.com/stsysd/sougen/heatmap"
)

func main() {
	// Generate sample data for one year
	data := generateYearData()

	// Create SVG heatmap
	svg := heatmap.GenerateYearlyHeatmapSVG(data, nil)

	// Output to stdout
	fmt.Println(svg)
}

// generateYearData creates random activity data for the past year
func generateYearData() []heatmap.Data {
	// Start from one year ago
	endDate := time.Now()
	startDate := endDate.AddDate(-1, 0, 0)

	// Create data array in ascending order (newest last)
	var data []heatmap.Data

	// Fill with data for each day
	current := startDate
	for !current.After(endDate) {
		// Generate random count
		// Higher probability of activity on weekends
		var count int
		if current.Weekday() == time.Saturday || current.Weekday() == time.Sunday {
			count = rand.Intn(10) // 0-9
		} else {
			count = rand.Intn(6) // 0-5
		}

		// Add occasional spikes of activity
		if rand.Intn(20) == 0 {
			count += rand.Intn(20) // Add 0-19 additional counts
		}

		if count != 0 {
			// Add the data point
			data = append(data, heatmap.Data{
				Date:  current,
				Value: count,
			})
		}

		// Move to previous day
		current = current.AddDate(0, 0, 1)
	}

	return data
}
