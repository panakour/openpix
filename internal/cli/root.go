// Package cli builds the cobra command tree and drives the TUI.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
	"github.com/panakour/openpix/internal/download"
	"github.com/panakour/openpix/internal/httpx"
	"github.com/panakour/openpix/internal/provider"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var errOneOrMoreFailed = errors.New("one or more downloads failed")

// Execute runs the root command and returns a process exit code.
func Execute() int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	err := newApp().rootCmd().ExecuteContext(ctx)

	switch {
	case err == nil:
		return 0
	case errors.Is(err, context.Canceled):
		fmt.Fprintln(os.Stderr, styleHighlight.Render("canceled"))

		return 130
	default:
		fmt.Fprintln(os.Stderr, styleDanger.Render("error: "+err.Error()))

		return 1
	}
}

type options struct {
	path      string
	count     int
	query     string
	jobs      int
	minWidth  int
	minHeight int
	license   string
	size      string
	silent    bool
	verbose   bool
}

type app struct {
	opts options
	http *httpx.Client
}

func newApp() *app {
	return &app{
		http: httpx.New(fmt.Sprintf("openpix/%s (+https://github.com/panakour/openpix)", version)),
	}
}

func (a *app) rootCmd() *cobra.Command {
	// nolint: exhaustruct
	root := &cobra.Command{
		Use:   "openpix",
		Short: "Download open-licensed images from Wikimedia Commons and Openverse",
		Long: "openpix downloads open-licensed images from Wikimedia Commons and Openverse. No API keys required.\n\n" +
			"Choose a source:\n" +
			"  openpix wikimedia           # random Featured Pictures\n" +
			"  openpix wikimedia -q desert # search Wikimedia Commons\n" +
			"  openpix openverse           # CC-licensed images from Openverse",
		Version:       Version(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	a.opts.bind(root.PersistentFlags())

	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if a.opts.jobs <= 0 {
			return errors.New("--jobs must be greater than 0")
		}

		// --size supersedes the default min-filters (1280×720) so "small" isn't
		// silently filtered out; explicit --min-* still wins.
		if a.opts.size != "" {
			if !cmd.Flags().Changed("min-width") {
				a.opts.minWidth = 0
			}

			if !cmd.Flags().Changed("min-height") {
				a.opts.minHeight = 0
			}
		}

		return a.opts.toQuery().Validate()
	}

	root.AddCommand(a.wikimediaCmd(), a.openverseCmd(), newVersionCmd())

	return root
}

func (a *app) wikimediaCmd() *cobra.Command {
	// nolint: exhaustruct
	return &cobra.Command{
		Use:     "wikimedia",
		Aliases: []string{"wm", "wmc", "wiki", "wikicommons", "commons"},
		Short:   "Download images from Wikimedia Commons",
		Long:    "Download random Featured Pictures when no query is given, or run a full-text file search when one is.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.run(cmd.Context(), provider.NewWikimedia(a.http))
		},
	}
}

func (a *app) openverseCmd() *cobra.Command {
	// nolint: exhaustruct
	cmd := &cobra.Command{
		Use:     "openverse",
		Aliases: []string{"o", "ov"},
		Short:   "Download CC-licensed images from Openverse",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.run(cmd.Context(), provider.NewOpenverse(a.http))
		},
	}

	cmd.Flags().StringVarP(&a.opts.license, "license", "l", "", "Comma-separated licenses (for example: cc0,by,by-sa)")

	return cmd
}

// run uses Bubble Tea on a TTY; runHeadless otherwise.
func (a *app) run(ctx context.Context, p provider.Provider) error {
	query := a.opts.toQuery()

	if a.opts.silent || !isatty.IsTerminal(os.Stdout.Fd()) {
		return a.runHeadless(ctx, p, query)
	}

	m := newModel(p.Name(), query, a.opts.path, a.opts.verbose)

	prog := tea.NewProgram(m, tea.WithContext(ctx))

	go a.worker(ctx, p, query, prog)

	final, err := prog.Run()
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	mf := final.(*model)

	if mf.searchErr != nil {
		return fmt.Errorf("%s: %w", p.Name(), mf.searchErr)
	}

	return mf.finalError()
}

func (a *app) runHeadless(ctx context.Context, p provider.Provider, q provider.Query) error {
	images, err := searchNonEmptyImages(ctx, p, q)
	if err != nil {
		return fmt.Errorf("%s: %w", p.Name(), err)
	}

	var failed atomic.Int32

	pipeline := a.newPipeline(func(e download.Event) {
		if e.Status == download.StatusFailed {
			failed.Add(1)
		}
	})

	if err := pipeline.Run(ctx, images); err != nil {
		return err
	}

	if failed.Load() > 0 {
		return errOneOrMoreFailed
	}

	return nil
}

func (a *app) worker(ctx context.Context, p provider.Provider, q provider.Query, prog *tea.Program) {
	images, err := searchNonEmptyImages(ctx, p, q)
	if err != nil {
		prog.Send(searchFailMsg{err: err})

		return
	}

	prog.Send(searchDoneMsg{images: images})

	pipeline := a.newPipeline(func(e download.Event) { prog.Send(e) })

	prog.Send(runDoneMsg{err: pipeline.Run(ctx, images)})
}

func (a *app) newPipeline(onEvent func(download.Event)) *download.Pipeline {
	return &download.Pipeline{
		HTTP:        a.http,
		OutDir:      a.opts.path,
		Concurrency: a.opts.jobs,
		OnEvent:     onEvent,
	}
}

func searchNonEmptyImages(ctx context.Context, p provider.Provider, q provider.Query) ([]provider.Image, error) {
	images, err := p.Search(ctx, q)
	if err != nil {
		return nil, err
	}

	if len(images) == 0 {
		return nil, emptyResultErr(q)
	}

	return images, nil
}

func emptyResultErr(q provider.Query) error {
	filters := make([]string, 0, 3)

	if q.SizeBucket != "" {
		filters = append(filters, "size "+q.SizeBucket)
	}

	switch {
	case q.MinWidth > 0 && q.MinHeight > 0:
		filters = append(filters, fmt.Sprintf("min %d×%d", q.MinWidth, q.MinHeight))
	case q.MinWidth > 0:
		filters = append(filters, fmt.Sprintf("min width %d", q.MinWidth))
	case q.MinHeight > 0:
		filters = append(filters, fmt.Sprintf("min height %d", q.MinHeight))
	}

	if q.License != "" {
		filters = append(filters, "license "+q.License)
	}

	if len(filters) > 0 {
		return fmt.Errorf("no images matched (%s; try relaxing the filters)", strings.Join(filters, ", "))
	}

	return errors.New("no images matched")
}

func (o *options) bind(fs *pflag.FlagSet) {
	fs.StringVarP(&o.path, "path", "p", defaultOutDir(), "Directory to save images in")
	fs.IntVarP(&o.count, "number", "n", 5, "Number of images to download")
	fs.StringVarP(&o.query, "query", "q", "", "Search term (empty means random or curated results)")
	fs.IntVarP(&o.jobs, "jobs", "j", 4, "Maximum concurrent downloads")
	fs.IntVar(&o.minWidth, "min-width", 1280, "Minimum image width in pixels (0 = no filter)")
	fs.IntVar(&o.minHeight, "min-height", 720, "Minimum image height in pixels (0 = no filter)")
	fs.StringVar(&o.size, "size", "", "Width bucket: small, medium, or large")
	fs.BoolVar(&o.silent, "silent", false, "Hide progress output; only print errors")
	fs.BoolVarP(&o.verbose, "verbose", "v", false, "Show the preview table and extra detail")
}

func (o *options) toQuery() provider.Query {
	return provider.Query{
		Term:       o.query,
		Count:      o.count,
		MinWidth:   o.minWidth,
		MinHeight:  o.minHeight,
		License:    o.license,
		SizeBucket: o.size,
	}
}

func defaultOutDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "openpix"
	}

	return filepath.Join(home, "Pictures", "openpix")
}
