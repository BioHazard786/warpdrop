package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/BioHazard786/Warpdrop/cli/internal/utils"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"golang.org/x/term"
)

/* -------------------------------------------------------------------------- */
/*                                   Helpers                                  */
/* -------------------------------------------------------------------------- */

func terminalWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 80
	}
	return w
}

func tableStyle() *table.Table {
	return table.New().
		Wrap(true).
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(Primary)).
		StyleFunc(func(row, _ int) lipgloss.Style {
			switch {
			case row == table.HeaderRow:
				return TableHeaderStyle
			case row%2 == 0:
				return TableRowStyle
			default:
				return TableRowAltStyle
			}
		})
}

func tableWidth(headers []string, rows [][]string) int {
	colWidths := make([]int, len(headers))

	for i, h := range headers {
		colWidths[i] = lipgloss.Width(h)
	}

	for _, row := range rows {
		for i, cell := range row {
			if w := lipgloss.Width(cell); w > colWidths[i] {
				colWidths[i] = w
			}
		}
	}

	width := 0
	for _, w := range colWidths {
		width += w
	}

	// column separators and padding  and outer borders
	return width + (len(headers) - 1) + (len(headers) * 2) + 2
}

func boxContentWidth(box lipgloss.Style, content string) int {
	lines := strings.Split(content, "\n")

	max := 0
	for _, line := range lines {
		w := lipgloss.Width(line)
		if w > max {
			max = w
		}
	}

	return max + box.GetHorizontalFrameSize()
}

/* -------------------------------------------------------------------------- */
/*                                 File Table                                 */
/* -------------------------------------------------------------------------- */

type FileTableItem struct {
	Index int
	Name  string
	Size  int64
	Type  string
}

type FileTable struct {
	items    []FileTableItem
	showType bool
}

func NewFileTable(items []FileTableItem) *FileTable {
	return &FileTable{
		items:    items,
		showType: true,
	}
}

func (t *FileTable) HideType() *FileTable {
	t.showType = false
	return t
}

func (t *FileTable) View() string {
	if len(t.items) == 0 {
		return MutedStyle.Render("No files")
	}

	headers := []string{"#", "Name", "Size"}
	if t.showType {
		headers = append(headers, "Type")
	}

	rows := make([][]string, 0, len(t.items))
	for _, item := range t.items {
		row := []string{
			fmt.Sprintf("%d", item.Index),
			item.Name,
			utils.FormatSize(item.Size),
		}

		if t.showType {
			row = append(row, item.Type)
		}

		rows = append(rows, row)
	}

	tbl := tableStyle().
		Headers(headers...).
		Rows(rows...)

	if w := tableWidth(headers, rows); w > terminalWidth() {
		tbl = tbl.Width(terminalWidth())
	}

	return tbl.Render()
}

func (t *FileTable) Render() {
	fmt.Println(t.View())
}

func RenderFileTable(items []FileTableItem) {
	fmt.Println(NewFileTable(items).View())
}

/* -------------------------------------------------------------------------- */
/*                             Transfer Summary                                */
/* -------------------------------------------------------------------------- */

type TransferSummary struct {
	Status    string
	Files     int
	TotalSize string
	Duration  string
	Speed     string
}

func NewTransferSummary(summary TransferSummary) *TransferSummary {
	return &TransferSummary{
		Status:    summary.Status,
		Files:     summary.Files,
		TotalSize: summary.TotalSize,
		Duration:  summary.Duration,
		Speed:     summary.Speed,
	}
}

func (t *TransferSummary) View() string {
	headers := []string{"Metric", "Value"}

	rows := [][]string{
		{"Status", t.Status},
		{"Files", fmt.Sprintf("%d", t.Files)},
		{"Total Size", t.TotalSize},
		{"Duration", t.Duration},
		{"Avg Speed", t.Speed},
	}

	tbl := tableStyle().
		Headers(headers...).
		Rows(rows...)

	if w := tableWidth(headers, rows); w > terminalWidth() {
		tbl = tbl.Width(terminalWidth())
	}

	return tbl.Render()
}

func RenderTransferSummary(summary TransferSummary) {
	fmt.Println(NewTransferSummary(summary).View())
}

/* -------------------------------------------------------------------------- */
/*                                  Room Info                                 */
/* -------------------------------------------------------------------------- */

type RoomInfo struct {
	Code string // "4821-amber-otter-kite-moth"
	Link string
}

func NewRoomInfo(code, link string) *RoomInfo { return &RoomInfo{Code: code, Link: link} }

func (r *RoomInfo) View() string {
	rows := []string{
		fmt.Sprintf("%s Code:    %s", IconCopy, BoldStyle.Foreground(Primary).Render(r.Code)),
		fmt.Sprintf("%s Link:    %s", IconWeb, r.Link),
		fmt.Sprintf("%s Receive: %s", IconReceive, "warpdrop receive "+r.Code),
	}
	content := fmt.Sprintf("%s Ready to share\n\n%s", IconSuccess, strings.Join(rows, "\n"))

	// Size the box to its content. If even that does not fit the terminal, skip the box: a wrapped link inside a
	// border cannot be copied, while plain soft-wrapped text can.
	box := SuccessBoxStyle.UnsetWidth()
	if boxContentWidth(box, content) > terminalWidth() {
		return content
	}
	return box.Render(content)
}

func RenderRoomInfo(code, link string) { fmt.Println(NewRoomInfo(code, link).View()) }
