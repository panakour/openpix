package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/panakour/openpix/internal/download"
	"github.com/panakour/openpix/internal/provider"
)

var (
	colBrand     = lipgloss.AdaptiveColor{Light: "#0E7490", Dark: "#22D3EE"}
	colDim       = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}
	colAccent    = lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#C084FC"}
	colHighlight = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FBBF24"}
	colSuccess   = lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#4ADE80"}
	colDanger    = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}

	styleBrand     = lipgloss.NewStyle().Foreground(colBrand).Bold(true)
	styleDim       = lipgloss.NewStyle().Foreground(colDim)
	styleAccent    = lipgloss.NewStyle().Foreground(colAccent)
	styleHighlight = lipgloss.NewStyle().Foreground(colHighlight)
	styleSuccess   = lipgloss.NewStyle().Foreground(colSuccess)
	styleDanger    = lipgloss.NewStyle().Foreground(colDanger)
	styleTableHdr  = lipgloss.NewStyle().Bold(true).Foreground(colDim)
)

const (
	indent          = "  "
	defaultWidth    = 80
	minBarWidth     = 10
	maxBarWidth     = 60
	narrowThreshold = 50
)

type (
	searchDoneMsg struct{ images []provider.Image }
	searchFailMsg struct{ err error }
	runDoneMsg    struct{ err error }
)

type phase int

const (
	phaseSearch phase = iota
	phaseDownload
	phaseDone
)

type model struct {
	phase   phase
	verbose bool

	source string
	query  provider.Query
	outDir string

	width int

	spinner  spinner.Model
	progress progress.Model

	images                []provider.Image
	total                 int
	done, skipped, failed int
	totalBytes            int64
	errorsList            []download.Event
	started               time.Time

	searchErr error
	runErr    error
}

func newModel(source string, q provider.Query, outDir string, verbose bool) *model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colAccent)

	pb := progress.New(progress.WithDefaultGradient(), progress.WithoutPercentage(), progress.WithWidth(40))

	return &model{
		phase:    phaseSearch,
		verbose:  verbose,
		source:   source,
		query:    q,
		outDir:   outDir,
		width:    defaultWidth,
		spinner:  sp,
		progress: pb,
	}
}

func (m *model) Init() tea.Cmd { return m.spinner.Tick }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case searchDoneMsg:
		m.images = msg.images
		m.total = len(msg.images)
		m.phase = phaseDownload
		m.started = time.Now()

		return m, nil

	case searchFailMsg:
		m.searchErr = msg.err
		m.phase = phaseDone

		return m, tea.Quit

	case download.Event:
		switch msg.Status {
		case download.StatusDone:
			m.done++
			m.totalBytes += msg.Bytes
		case download.StatusSkipped:
			m.skipped++
			m.totalBytes += msg.Bytes
		case download.StatusFailed:
			m.failed++
			m.errorsList = append(m.errorsList, msg)
		}

		return m, m.progress.SetPercent(float64(m.done+m.skipped+m.failed) / float64(m.total))

	case runDoneMsg:
		m.runErr = msg.err
		m.phase = phaseDone

		return m, tea.Quit

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)

		return m, cmd

	case progress.FrameMsg:
		pm, cmd := m.progress.Update(msg)
		if next, ok := pm.(progress.Model); ok {
			m.progress = next
		}

		return m, cmd

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.progress.Width = clampBarWidth(msg.Width - 20)

		return m, nil
	}

	return m, nil
}

func clampBarWidth(w int) int {
	return min(max(w, minBarWidth), maxBarWidth)
}

func (m *model) View() string {
	var b strings.Builder

	b.WriteString("\n" + indent + m.renderHeader() + "\n")
	b.WriteString(indent + styleDim.Render("→ "+m.outDir) + "\n\n")

	switch m.phase {
	case phaseSearch:
		b.WriteString(indent + m.spinner.View() + " searching " + m.source + "…\n")

	case phaseDownload:
		b.WriteString(indent + m.searchSummary() + "\n")

		if m.verbose {
			b.WriteString("\n" + m.preview() + "\n")
		}

		b.WriteString("\n" + indent + "downloading " + m.progress.View())
		b.WriteString("  " + styleDim.Render(fmt.Sprintf("%d/%d", m.done+m.skipped+m.failed, m.total)) + "\n")

		if live := m.liveCounters(); live != "" {
			b.WriteString(indent + live + "\n")
		}

	case phaseDone:
		if m.searchErr != nil {
			b.WriteString(indent + styleDanger.Render("✗ ") + m.searchErr.Error() + "\n")
			break
		}

		b.WriteString(indent + m.searchSummary() + "\n")

		if m.verbose {
			b.WriteString("\n" + m.preview() + "\n")
		}

		b.WriteString("\n" + m.finalSummary() + "\n")
	}

	return b.String()
}

func (m *model) renderHeader() string {
	if m.width > 0 && m.width < narrowThreshold {
		return styleBrand.Render("openpix") + " " + styleAccent.Render(m.source)
	}

	parts := []string{
		styleBrand.Render("openpix"),
		styleDim.Render("·"),
		styleAccent.Render(m.source),
		styleDim.Render("·"),
	}

	if m.query.Term != "" {
		parts = append(parts, styleHighlight.Render(fmt.Sprintf("%q", m.query.Term)))
	} else {
		parts = append(parts, styleDim.Render("random"))
	}

	return strings.Join(parts, " ")
}

func (m *model) searchSummary() string {
	summary := fmt.Sprintf("found %d image%s", len(m.images), plural(len(m.images)))

	if total := totalBytes(m.images); total > 0 {
		summary += styleDim.Render(fmt.Sprintf(" (%s)", humanBytes(total)))
	}

	return styleSuccess.Render("✓") + " " + summary
}

func (m *model) liveCounters() string {
	if m.done+m.skipped+m.failed == 0 {
		return ""
	}

	parts := []string{}

	if m.done > 0 {
		parts = append(parts, styleSuccess.Render(fmt.Sprintf("✓ %d", m.done)))
	}

	if m.skipped > 0 {
		parts = append(parts, styleHighlight.Render(fmt.Sprintf("↺ %d", m.skipped)))
	}

	if m.failed > 0 {
		parts = append(parts, styleDanger.Render(fmt.Sprintf("✗ %d", m.failed)))
	}

	return strings.Join(parts, "  ")
}

func (m *model) finalSummary() string {
	var out strings.Builder

	errBudget := max(m.width-6, 30)
	titleBudget := errBudget / 3
	msgBudget := errBudget - titleBudget

	for _, e := range m.errorsList {
		out.WriteString(indent + styleDanger.Render("✗ "))
		out.WriteString(truncate(e.Image.Title, titleBudget))
		out.WriteString(styleDim.Render(" — "))
		out.WriteString(truncate(simplifyErr(e.Err), msgBudget))
		out.WriteString("\n")
	}

	if len(m.errorsList) > 0 {
		out.WriteString("\n")
	}

	parts := []string{}

	if m.done > 0 {
		parts = append(parts, styleSuccess.Render(fmt.Sprintf("✓ %d downloaded", m.done)))
	}

	if m.skipped > 0 {
		parts = append(parts, styleHighlight.Render(fmt.Sprintf("↺ %d skipped", m.skipped)))
	}

	if m.failed > 0 {
		parts = append(parts, styleDanger.Render(fmt.Sprintf("✗ %d failed", m.failed)))
	}

	if len(parts) == 0 {
		parts = append(parts, styleDim.Render("nothing to do"))
	}

	elapsed := time.Since(m.started).Round(100 * time.Millisecond)

	out.WriteString(indent + strings.Join(parts, "   ") + "  ")
	out.WriteString(styleDim.Render(fmt.Sprintf("(%s in %s)", humanBytes(m.totalBytes), elapsed)))

	return out.String()
}

func (m *model) preview() string {
	cols := []int{50, 11, 10, 20}

	rows := make([]string, 0, 1+len(m.images))
	rows = append(rows, indent+renderRow([]string{"title", "dim", "size", "license"}, cols, styleTableHdr))

	for _, img := range m.images {
		size := "?"
		if img.SizeBytes > 0 {
			size = humanBytes(img.SizeBytes)
		}

		dim := "?"
		if img.Width > 0 && img.Height > 0 {
			dim = fmt.Sprintf("%d×%d", img.Width, img.Height)
		}

		rows = append(rows, indent+renderRow([]string{truncate(img.Title, cols[0]), dim, size, img.License}, cols, lipgloss.NewStyle()))
	}

	return strings.Join(rows, "\n")
}

func renderRow(cells []string, widths []int, style lipgloss.Style) string {
	padded := make([]string, len(cells))

	for i, c := range cells {
		padded[i] = style.Render(padRight(c, widths[i]))
	}

	return strings.Join(padded, "  ")
}

// finalError returns the first terminal error from the run, unwrapped.
// Callers wrap the search error with the provider name to match the headless
// code path.
func (m *model) finalError() error {
	switch {
	case m.searchErr != nil:
		return m.searchErr
	case m.runErr != nil:
		return m.runErr
	case m.failed > 0:
		return errOneOrMoreFailed
	}

	return nil
}

func totalBytes(images []provider.Image) int64 {
	var t int64
	for _, img := range images {
		t += img.SizeBytes
	}

	return t
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	div, exp := int64(unit), 0

	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// truncate shortens s to width display columns, appending "…" when cut.
func truncate(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}

	if width <= 1 {
		return "…"
	}

	runes := []rune(s)
	for i := len(runes); i > 0; i-- {
		candidate := string(runes[:i]) + "…"
		if lipgloss.Width(candidate) <= width {
			return candidate
		}
	}

	return "…"
}

// simplifyErr maps common net/http noise to short labels. Typed matches win;
// a string fallback covers errors that lost their sentinel through wrapping
// (e.g. errors formatted by third-party libraries).
func simplifyErr(err error) string {
	if err == nil {
		return ""
	}

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timed out"
	case errors.Is(err, context.Canceled):
		return "canceled"
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "dns lookup failed"
	}

	s := err.Error()

	switch {
	case strings.Contains(s, "context deadline exceeded"):
		return "timed out"
	case strings.Contains(s, "context canceled"):
		return "canceled"
	case strings.Contains(s, "no such host"):
		return "dns lookup failed"
	case strings.Contains(s, "connection refused"):
		return "connection refused"
	}

	if i := strings.Index(s, `Get "`); i >= 0 {
		if j := strings.Index(s[i:], `": `); j >= 0 {
			s = s[:i] + s[i+j+3:]
		}
	}

	return s
}

func padRight(s string, width int) string {
	if w := lipgloss.Width(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}

	return s
}

func plural(n int) string {
	if n == 1 {
		return ""
	}

	return "s"
}
