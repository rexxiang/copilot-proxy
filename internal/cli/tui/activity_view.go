package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type ActivityGranularity int

const (
	GranularityWeek ActivityGranularity = iota
	GranularityMonth
	GranularityYear
)

const (
	granularityWeek  = GranularityWeek
	granularityMonth = GranularityMonth
	granularityYear  = GranularityYear

	hoursPerDay      = 24
	hourLabelStep    = 2
	hourLabelCount   = hoursPerDay / hourLabelStep
	dayCount         = 7
	daysPerWeek      = 7
	weeksInMonth     = 5
	monthDays        = weeksInMonth * daysPerWeek
	weekStartOffset  = dayCount - 1
	monthHeaderWidth = 60
	yearHeaderWidth  = 50

	monthThresholdLow  = 25
	monthThresholdMed  = 50
	monthThresholdHigh = 100
	yearThresholdLow   = 100
	yearThresholdMed   = 500
	yearThresholdHigh  = 1000
	monthsInYear       = 12
	yearBarWidth       = 30
)

type ActivityView struct {
	width       int
	height      int
	state       *SharedState
	granularity ActivityGranularity
}

func NewActivityView() *ActivityView {
	return &ActivityView{
		granularity: granularityWeek, // Default to week view
	}
}

func (v *ActivityView) Update(msg tea.Msg) (ViewComponent, tea.Cmd) {
	return v, nil
}

func (v *ActivityView) View() string {
	var sb strings.Builder
	sb.WriteString("\n")

	granularityNames := []string{"Week", "Month", "Year"}

	// Show all modes with current one highlighted
	var modeTabs []string
	for i, name := range granularityNames {
		if ActivityGranularity(i) == v.granularity {
			modeTabs = append(modeTabs, SelectedTabStyle.Render(" "+name+" "))
		} else {
			modeTabs = append(modeTabs, TabStyle.Render(" "+name+" "))
		}
	}

	sb.WriteString(fmt.Sprintf("Activity Heatmap  %s  %s\n\n",
		strings.Join(modeTabs, ""),
		DimStyle.Render("(<,> to switch)")))

	// Use ActivityHour for week view, ActivityDay for month/year views
	var activityMap map[time.Time]int
	switch v.granularity {
	case granularityWeek: // Week - use hourly data
		activityMap = v.state.Snapshot.ActivityHour
	case granularityMonth, granularityYear: // Month, Year - use daily data
		activityMap = v.state.Snapshot.ActivityDay
	}

	if activityMap == nil {
		activityMap = make(map[time.Time]int)
	}

	// Find max count for scaling
	maxCount := 1
	for _, count := range activityMap {
		if count > maxCount {
			maxCount = count
		}
	}

	// Render heatmap based on granularity
	switch v.granularity {
	case granularityWeek: // Week
		sb.WriteString(v.renderWeekHeatmap(activityMap, maxCount))
	case granularityMonth: // Month
		sb.WriteString(v.renderMonthHeatmap(activityMap))
	case granularityYear: // Year
		sb.WriteString(v.renderYearHeatmap(activityMap))
	}

	// Legend with granularity-specific thresholds
	sb.WriteString("\n")
	sb.WriteString(DimStyle.Render("Legend: "))
	sb.WriteString(HeatNone.Render("■"))
	sb.WriteString(" 0  ")

	switch v.granularity {
	case granularityWeek: // Week - hourly data, smaller thresholds
		sb.WriteString(HeatLow.Render("■"))
		sb.WriteString(" 1-10  ")
		sb.WriteString(HeatMed.Render("■"))
		sb.WriteString(" 11-25  ")
		sb.WriteString(HeatHigh.Render("■"))
		sb.WriteString(" 26-50  ")
		sb.WriteString(HeatMax.Render("■"))
		sb.WriteString(" 50+")
	case granularityMonth: // Month - daily data, medium thresholds
		sb.WriteString(HeatLow.Render("■"))
		sb.WriteString(" 1-25  ")
		sb.WriteString(HeatMed.Render("■"))
		sb.WriteString(" 26-50  ")
		sb.WriteString(HeatHigh.Render("■"))
		sb.WriteString(" 51-100  ")
		sb.WriteString(HeatMax.Render("■"))
		sb.WriteString(" 100+")
	case granularityYear: // Year - monthly data, larger thresholds
		sb.WriteString(HeatLow.Render("■"))
		sb.WriteString(" 1-100  ")
		sb.WriteString(HeatMed.Render("■"))
		sb.WriteString(" 101-500  ")
		sb.WriteString(HeatHigh.Render("■"))
		sb.WriteString(" 501-1000  ")
		sb.WriteString(HeatMax.Render("■"))
		sb.WriteString(" 1000+")
	}

	return sb.String()
}

func (v *ActivityView) SetSize(width, height int) {
	v.width = width
	v.height = height
}

func (v *ActivityView) SetState(state *SharedState) {
	v.state = state
}

func (v *ActivityView) HandleKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if key.Matches(msg, key.NewBinding(key.WithKeys("home"))) {
		v.granularity = granularityWeek
		return true, nil
	}
	if key.Matches(msg, key.NewBinding(key.WithKeys("end"))) {
		v.granularity = granularityYear
		return true, nil
	}
	if key.Matches(msg, key.NewBinding(key.WithKeys(",", "，", "<", "《"))) {
		// Previous mode (cycle backwards)
		if v.granularity > granularityWeek {
			v.granularity--
		} else {
			v.granularity = granularityYear // Wrap to Year
		}
		return true, nil
	}
	if key.Matches(msg, key.NewBinding(key.WithKeys(".", "。", ">", "》"))) {
		// Next mode (cycle forwards)
		if v.granularity < granularityYear {
			v.granularity++
		} else {
			v.granularity = granularityWeek // Wrap to Week
		}
		return true, nil
	}
	return false, nil
}

func (v *ActivityView) VisibleLines() int {
	if v.height <= 0 {
		return 0
	}
	return v.height
}

// renderWeekHeatmap shows last 7 days x 24 hours (hourly data).
func (v *ActivityView) renderWeekHeatmap(data map[time.Time]int, maxCount int) string {
	var sb strings.Builder

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	// Header: hours (every 2 hours to fit width)
	// Each hour cell is 2 chars ("■ "), so every 2 hours = 4 chars, use "%02d  " (4 chars) to align
	sb.WriteString("       ")
	for h := range hourLabelCount {
		hour := h * hourLabelStep
		sb.WriteString(fmt.Sprintf("%02d  ", hour))
	}
	sb.WriteString("\n")

	// Days (oldest first, matching Month/Year views)
	for d := range dayCount {
		day := today.AddDate(0, 0, d-weekStartOffset)
		sb.WriteString(day.Format("01-02") + "  ")
		for h := range hoursPerDay {
			hourKey := time.Date(day.Year(), day.Month(), day.Day(), h, 0, 0, 0, day.Location())
			count := data[hourKey]
			sb.WriteString(HeatmapCell(count, maxCount))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// renderMonthHeatmap shows last 5 weeks in compact calendar style (daily data)
// Week starts on Monday.
func (v *ActivityView) renderMonthHeatmap(data map[time.Time]int) string {
	var sb strings.Builder

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	// Find max for scaling
	maxDaily := 1
	for d := range monthDays {
		day := today.AddDate(0, 0, -d)
		dayKey := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
		if count := data[dayKey]; count > maxDaily {
			maxDaily = count
		}
	}

	// Find the start of this week (Monday = 1, so we calculate days since Monday)
	// Go's Weekday: Sunday=0, Monday=1, ..., Saturday=6
	// Days to subtract to get to Monday: (weekday + 6) % 7
	weekday := int(today.Weekday())
	daysToMonday := (weekday + (daysPerWeek - 1)) % daysPerWeek
	startOfWeek := today.AddDate(0, 0, -daysToMonday)
	startDay := startOfWeek.AddDate(0, 0, -(weeksInMonth-1)*daysPerWeek) // 5 weeks back

	// Header: days of week (Monday first)
	sb.WriteString("        Mon   Tue   Wed   Thu   Fri   Sat   Sun\n")
	sb.WriteString(strings.Repeat("-", monthHeaderWidth))
	sb.WriteString("\n")

	// 5 weeks (oldest first)
	for w := range weeksInMonth {
		weekStart := startDay.AddDate(0, 0, w*daysPerWeek)
		sb.WriteString(weekStart.Format("01/02") + "  ")

		for dow := range daysPerWeek {
			day := weekStart.AddDate(0, 0, dow)
			dayKey := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())

			// Don't show future dates
			if day.After(today) {
				sb.WriteString("   -  ")
				continue
			}

			count := data[dayKey]

			// Format count with color (adjusted thresholds for daily data)
			var styledCount string
			countStr := fmt.Sprintf("%4d", count)
			switch {
			case count == 0:
				styledCount = DimStyle.Render(countStr)
			case count <= monthThresholdLow:
				styledCount = HeatLow.Render(countStr)
			case count <= monthThresholdMed:
				styledCount = HeatMed.Render(countStr)
			case count <= monthThresholdHigh:
				styledCount = HeatHigh.Render(countStr)
			default:
				styledCount = HeatMax.Render(countStr)
			}
			sb.WriteString(styledCount + "  ")
		}
		sb.WriteString("\n")
	}

	// Summary row
	sb.WriteString("\n")
	totalMonth := 0
	for d := range monthDays {
		day := today.AddDate(0, 0, -d)
		if day.Before(startDay) {
			continue
		}
		dayKey := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
		totalMonth += data[dayKey]
	}
	sb.WriteString(fmt.Sprintf("Total (5 weeks): %d requests\n", totalMonth))

	return sb.String()
}

// renderYearHeatmap shows last 12 months with monthly request bars (daily data aggregated).
func (v *ActivityView) renderYearHeatmap(data map[time.Time]int) string {
	var sb strings.Builder

	now := time.Now()

	// Calculate monthly totals
	monthlyTotals := make(map[string]int)
	maxMonthly := 1
	for m := range monthsInYear {
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -m, 0)
		nextMonth := monthStart.AddDate(0, 1, 0)
		monthKey := monthStart.Format("2006-01")

		total := 0
		for d := monthStart; d.Before(nextMonth); d = d.AddDate(0, 0, 1) {
			dayKey := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, d.Location())
			total += data[dayKey]
		}
		monthlyTotals[monthKey] = total
		if total > maxMonthly {
			maxMonthly = total
		}
	}

	// Header
	sb.WriteString("Month      Requests\n")
	sb.WriteString(strings.Repeat("-", yearHeaderWidth))
	sb.WriteString("\n")

	// Last 12 months (newest first)
	for m := range monthsInYear {
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -m, 0)
		monthKey := monthStart.Format("2006-01")
		count := monthlyTotals[monthKey]

		// Month label
		monthStr := monthStart.Format("Jan 2006")

		// Progress bar (width 30)
		barWidth := yearBarWidth
		filled := 0
		if maxMonthly > 0 {
			filled = count * barWidth / maxMonthly
		}
		if filled > barWidth {
			filled = barWidth
		}

		// Apply styling based on count
		var styledBar string
		switch {
		case count == 0:
			styledBar = DimStyle.Render(strings.Repeat("░", barWidth))
		case count <= yearThresholdLow:
			styledBar = HeatLow.Render(strings.Repeat("█", filled)) + DimStyle.Render(strings.Repeat("░", barWidth-filled))
		case count <= yearThresholdMed:
			styledBar = HeatMed.Render(strings.Repeat("█", filled)) + DimStyle.Render(strings.Repeat("░", barWidth-filled))
		case count <= yearThresholdHigh:
			styledBar = HeatHigh.Render(strings.Repeat("█", filled)) + DimStyle.Render(strings.Repeat("░", barWidth-filled))
		default:
			styledBar = HeatMax.Render(strings.Repeat("█", filled)) + DimStyle.Render(strings.Repeat("░", barWidth-filled))
		}

		sb.WriteString(fmt.Sprintf("%s  %s %5d\n", monthStr, styledBar, count))
	}

	return sb.String()
}
