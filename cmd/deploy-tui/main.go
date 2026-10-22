// deploy-tui — terminal app that provisions a fresh Linux VPS for a Laravel
// (or static / custom) project from end to end. Same logic the desktop app's
// "Apply" flow uses, but standalone — useful for one-off bootstrap runs and
// for testing the apply plan before it's exposed as a desktop feature.
//
// Build:  go build -o deploy-tui.exe ./cmd/deploy-tui
// Run:    ./deploy-tui.exe
//
// The form collects SSH credentials + deploy config; the TUI then connects,
// runs the detection probes, builds the apply plan, and executes each step
// with a live log feed and per-step status pills.
//
// NOTE on i18n: this is a separate binary from the desktop app (its own
// main(), no LoadCatalogues/SetGlobal call) and cannot embed the shared
// langs/ catalogue — go:embed can't reach "../../langs" from this package,
// and moving langs/ under an internal package would break the public-repo
// mirror path the translation system relies on. Errors here are deliberately
// plain fmt.Errorf with English text, not i18n.Errorf: without a loaded
// catalogue, an *i18n.Error renders as its bare key path (e.g.
// "errors.deploytui.passwordRequired") instead of prose, which is worse
// than leaving this binary untranslated. Do not convert these to i18n
// without first solving catalogue loading for this binary.
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"lara4ever/internal/config"
	"lara4ever/internal/deploy"
	pkgssh "lara4ever/internal/ssh"

	xssh "golang.org/x/crypto/ssh"
)

// ---------- Form (huh) ----------

type formInput struct {
	Address  string
	Port     string
	User     string
	AuthMode string // "password" | "key"
	Password string
	KeyPath  string

	Name        string
	Domain      string
	Environment string
	GitURL      string
	GitBranch   string
	GitPAT      string // optional — splices into HTTPS URL for private repos
	WebServer   string
	PHPVersion  string
	Database    string
	BuildScript string
	EnableSSL   bool
	LEEmail     string
	AutoDeploy  bool
	AutoCron    string
}

func defaultForm() *formInput {
	return &formInput{
		Port:        "22",
		User:        "root",
		AuthMode:    "password",
		Environment: "production",
		GitBranch:   "main",
		WebServer:   "nginx",
		// Default to 8.3 — universally available on sury.org for jammy/noble/focal/bookworm.
		// 8.4 works on most setups but not every codename has the full set of -bcmath/-zip/-mysql
		// satellite packages yet, and the user gets cryptic "no installation candidate" errors.
		// Override to 8.4 if you know your distro supports it.
		PHPVersion: "8.3",
		Database:   "",
		BuildScript: `# Bootstrap .env from .env.example on first deploy.
[ -f .env ] || cp .env.example .env

composer install --no-dev --optimize-autoloader

# APP_KEY only on first run.
grep -q '^APP_KEY=base64:' .env || php artisan key:generate --force

# Migrations — guarded so a misconfigured DB doesn't kill the whole deploy.
# Configure DB_* in .env and re-run if this warns.
php artisan migrate --force || echo "WARN: migrations skipped — set DB_* in .env"

php artisan config:cache
php artisan route:cache
php artisan view:cache`,
		AutoCron: "*/5 * * * *",
	}
}

func collectInput() (*formInput, error) {
	in := defaultForm()

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title("VPS connection").Description("Where do we deploy to?"),
			huh.NewInput().Title("Host (IP or domain)").Value(&in.Address).Validate(notEmpty("host")),
			huh.NewInput().Title("Port").Value(&in.Port),
			huh.NewInput().Title("User").Value(&in.User).Validate(notEmpty("user")),
			huh.NewSelect[string]().Title("Auth").Options(
				huh.NewOption("password", "password"),
				huh.NewOption("private key file", "key"),
			).Value(&in.AuthMode),
			huh.NewInput().Title("Password").Value(&in.Password).EchoMode(huh.EchoModePassword).
				DescriptionFunc(func() string { return "" }, &in.AuthMode).
				Validate(func(s string) error {
					if in.AuthMode == "password" && s == "" {
						return fmt.Errorf("password required")
					}
					return nil
				}),
			huh.NewInput().Title("Key path (absolute)").Value(&in.KeyPath).
				Validate(func(s string) error {
					if in.AuthMode == "key" && s == "" {
						return fmt.Errorf("key path required")
					}
					return nil
				}),
		),

		huh.NewGroup(
			huh.NewNote().Title("Project").Description("What goes on the server?"),
			huh.NewInput().Title("Deployment name (also /var/www/<name>)").Value(&in.Name).
				Validate(notEmpty("name")),
			huh.NewInput().Title("Domain (without prefix)").Placeholder("example.com").Value(&in.Domain).
				Validate(notEmpty("domain")),
			huh.NewSelect[string]().Title("Environment").Options(
				huh.NewOption("production (domain.com)", "production"),
				huh.NewOption("development (dev.domain.com)", "development"),
				huh.NewOption("staging (staging.domain.com)", "staging"),
			).Value(&in.Environment),
			huh.NewInput().Title("Git URL (HTTPS — public or private)").
				Description("Always paste the HTTPS URL: https://github.com/owner/repo.git. "+
					"Public repos: leave PAT empty. Private repos: fill the PAT field below. "+
					"SSH URLs (git@...) only work if you've manually set up a deploy key on the VPS.").
				Value(&in.GitURL).Validate(notEmpty("git URL")),
			huh.NewInput().Title("GitHub PAT — only for private repos").
				Description("Required if the repo is private. Generate at https://github.com/settings/tokens (Classic, scope: repo). "+
					"Leave empty for public repos. Spliced into the URL automatically — never written to disk on this machine.").
				Value(&in.GitPAT).EchoMode(huh.EchoModePassword),
			huh.NewInput().Title("Branch").Value(&in.GitBranch),
		),

		huh.NewGroup(
			huh.NewNote().Title("Stack").Description("nginx vs apache, PHP version, database."),
			huh.NewSelect[string]().Title("Web server").Options(
				huh.NewOption("nginx", "nginx"),
				huh.NewOption("apache", "apache"),
			).Value(&in.WebServer),
			huh.NewSelect[string]().Title("PHP version").
				Description("8.3 is the safe default. 8.4 requires Ubuntu 22.04+/Debian 12+ via sury.org.").
				Options(
					huh.NewOption("8.3 (recommended)", "8.3"),
					huh.NewOption("8.4", "8.4"),
					huh.NewOption("8.2", "8.2"),
					huh.NewOption("none (skip PHP)", ""),
				).Value(&in.PHPVersion),
			huh.NewSelect[string]().Title("Database").Options(
				huh.NewOption("none", ""),
				huh.NewOption("MySQL", "mysql"),
				huh.NewOption("PostgreSQL", "postgres"),
			).Value(&in.Database),
			huh.NewText().Title("Build script (post-pull)").Value(&in.BuildScript).Lines(4),
		),

		huh.NewGroup(
			huh.NewNote().Title("HTTPS & auto-deploy").Description("Optional but recommended."),
			huh.NewConfirm().Title("Enable HTTPS via Let's Encrypt?").Value(&in.EnableSSL),
			huh.NewInput().Title("Let's Encrypt email").Value(&in.LEEmail).
				Validate(func(s string) error {
					if in.EnableSSL && s == "" {
						return fmt.Errorf("email required when SSL enabled")
					}
					return nil
				}),
			huh.NewConfirm().Title("Auto-deploy on git push (cron-poll)?").Value(&in.AutoDeploy),
			huh.NewInput().Title("Cron expression").Value(&in.AutoCron),
		),
	).WithTheme(huh.ThemeCharm())

	if err := form.Run(); err != nil {
		return nil, err
	}
	return in, nil
}

func notEmpty(label string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s is required", label)
		}
		return nil
	}
}

// ---------- Conversion: form → SSHHost + ApplyConfig ----------

func toSSHHost(in *formInput) config.SSHHost {
	port, _ := strconv.Atoi(in.Port)
	if port == 0 {
		port = 22
	}
	h := config.SSHHost{
		Name:     in.Name,
		Address:  in.Address,
		Port:     port,
		User:     in.User,
		AuthMode: in.AuthMode,
		Password: in.Password,
	}
	if in.AuthMode == "key" {
		// internal/ssh expects a key file inside the per-host dir. Easiest
		// path: stash the absolute path as KeyName so the SSH lib treats it
		// as a literal location.
		h.KeyName = in.KeyPath
	}
	return h
}

func toApplyConfig(in *formInput) deploy.ApplyConfig {
	return deploy.ApplyConfig{
		Name:             in.Name,
		ProjectKind:      "laravel",
		Domain:           strings.TrimSpace(in.Domain),
		Environment:      in.Environment,
		GitURL:           deploy.SpliceGitPAT(deploy.NormalizeGitURL(in.GitURL), strings.TrimSpace(in.GitPAT)),
		GitBranch:        strings.TrimSpace(in.GitBranch),
		WebServer:        in.WebServer,
		PHPVersion:       in.PHPVersion,
		Database:         in.Database,
		BuildScript:      in.BuildScript,
		EnableSSL:        in.EnableSSL,
		LetsEncryptEmail: strings.TrimSpace(in.LEEmail),
		AutoDeploy:       in.AutoDeploy,
		AutoDeployCron:   strings.TrimSpace(in.AutoCron),
	}
}

// URL helpers (SpliceGitPAT, NormalizeGitURL) live in internal/deploy —
// shared between this CLI and the desktop app's Apply path so both behave
// identically when the user hands them a Git URL + PAT pair.

// ---------- Run screen (bubbletea) ----------

type stepStatus int

const (
	statusPending stepStatus = iota
	statusRunning
	statusDone
	statusError
)

type stepRow struct {
	Title  string
	Status stepStatus
}

type runModel struct {
	cfg  deploy.ApplyConfig
	host config.SSHHost
	plan []deploy.Step // SAME plan that produced the visible step list — must
	// not be rebuilt inside the goroutine (used to drift
	// when det filtering changed step counts).
	steps     []stepRow
	logCh     chan string
	doneCh    chan error
	logBuf    []string
	view      viewport.Model
	prog      progress.Model
	width     int
	height    int
	finished  bool
	finalErr  error
	startedAt time.Time
}

type (
	msgLine    string
	msgStepBeg int
	msgStepEnd struct {
		idx    int
		status stepStatus
	}
	msgFinished struct{ err error }
)

const maxLogLines = 4000

func newRunModel(host config.SSHHost, cfg deploy.ApplyConfig, steps []deploy.Step) *runModel {
	rows := make([]stepRow, 0, len(steps)+1)
	rows = append(rows, stepRow{Title: "Connect SSH", Status: statusRunning})
	for _, s := range steps {
		rows = append(rows, stepRow{Title: s.Title, Status: statusPending})
	}
	vp := viewport.New(80, 12)
	vp.SetContent("(connecting…)")
	pb := progress.New(progress.WithDefaultGradient(), progress.WithoutPercentage())
	pb.Width = 60
	return &runModel{
		cfg:       cfg,
		host:      host,
		plan:      steps,
		steps:     rows,
		logCh:     make(chan string, 256),
		doneCh:    make(chan error, 1),
		view:      vp,
		prog:      pb,
		startedAt: time.Now(),
	}
}

// progressFraction returns the % of steps done (or errored) over total.
// "done + errored" both count as completed for progress purposes — once a
// step fails the run halts, so the bar should freeze where it is, not
// pretend the rest is still in flight.
func (m *runModel) progressFraction() float64 {
	if len(m.steps) == 0 {
		return 0
	}
	completed := 0
	for _, s := range m.steps {
		if s.Status == statusDone || s.Status == statusError {
			completed++
		}
	}
	return float64(completed) / float64(len(m.steps))
}

// currentStepTitle returns the label of the step currently in progress,
// or empty when nothing is running. Used for the live caption above the
// progress bar.
func (m *runModel) currentStepTitle() string {
	for _, s := range m.steps {
		if s.Status == statusRunning {
			return s.Title
		}
	}
	return ""
}

func (m *runModel) Init() tea.Cmd {
	return tea.Batch(
		m.startWork(),
		m.waitForLine(),
	)
}

// startWork dials + executes the plan in a goroutine; emits stepBeg/stepEnd
// markers via the log channel by writing magic prefixes the parser splits on.
func (m *runModel) startWork() tea.Cmd {
	return func() tea.Msg {
		go func() {
			client, err := dialDirect(m.host)
			if err != nil {
				m.logCh <- "[ERR] " + err.Error()
				m.doneCh <- err
				close(m.logCh)
				return
			}
			defer client.Close()
			m.logCh <- "✓ SSH connected"
			m.logCh <- "<<<STEP_DONE 0>>>" // step 0 = Connect

			// Reuse the plan stored on the model — it was built once in main()
			// with the actual detection result, so step indices line up with
			// the visible step list. Rebuilding here with det=nil would inject
			// extra steps (apt update, Install missing packages on a fresh
			// VPS) and the index drift made wrong rows light up red.
			plan := m.plan
			for i, st := range plan {
				m.logCh <- fmt.Sprintf("<<<STEP_BEG %d>>>", i+1)
				m.logCh <- fmt.Sprintf("=== %s ===", st.Title)
				m.logCh <- "$ " + st.Cmd

				cmd := st.Cmd
				if st.Sudo {
					cmd = "sudo -n bash -lc " + shQuote(cmd)
				} else {
					cmd = "bash -lc " + shQuote(cmd)
				}
				if runErr := runStreaming(client, cmd, m.logCh); runErr != nil {
					if st.Ignore {
						m.logCh <- fmt.Sprintf("(non-zero ignored: %v)", runErr)
						m.logCh <- fmt.Sprintf("<<<STEP_DONE %d>>>", i+1)
						continue
					}
					m.logCh <- fmt.Sprintf("[ERR] %v", runErr)
					m.logCh <- fmt.Sprintf("<<<STEP_ERR %d>>>", i+1)
					m.doneCh <- fmt.Errorf("%s: %w", st.Title, runErr)
					close(m.logCh)
					return
				}
				m.logCh <- fmt.Sprintf("<<<STEP_DONE %d>>>", i+1)
			}

			m.logCh <- "✓ All steps completed"
			m.doneCh <- nil
			close(m.logCh)
		}()
		return nil
	}
}

func (m *runModel) waitForLine() tea.Cmd {
	return func() tea.Msg {
		select {
		case line, ok := <-m.logCh:
			if !ok {
				err := <-m.doneCh
				return msgFinished{err: err}
			}
			// Decode magic markers.
			if strings.HasPrefix(line, "<<<STEP_BEG ") {
				var i int
				fmt.Sscanf(line, "<<<STEP_BEG %d>>>", &i)
				return msgStepBeg(i)
			}
			if strings.HasPrefix(line, "<<<STEP_DONE ") {
				var i int
				fmt.Sscanf(line, "<<<STEP_DONE %d>>>", &i)
				return msgStepEnd{idx: i, status: statusDone}
			}
			if strings.HasPrefix(line, "<<<STEP_ERR ") {
				var i int
				fmt.Sscanf(line, "<<<STEP_ERR %d>>>", &i)
				return msgStepEnd{idx: i, status: statusError}
			}
			return msgLine(line)
		}
	}
}

func (m *runModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		stepsHeight := len(m.steps) + 4
		// +4 extra rows now: progress bar + caption + spacing
		vpHeight := msg.Height - stepsHeight - 8
		if vpHeight < 5 {
			vpHeight = 5
		}
		m.view.Width = msg.Width - 4
		m.view.Height = vpHeight
		m.view.SetContent(strings.Join(m.logBuf, "\n"))
		m.view.GotoBottom()
		// Re-size the progress bar to fit available width (capped to keep readable).
		w := msg.Width - 6
		if w > 80 {
			w = 80
		}
		if w < 20 {
			w = 20
		}
		m.prog.Width = w

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			if m.finished {
				return m, tea.Quit
			}
		}

	case msgLine:
		m.logBuf = append(m.logBuf, string(msg))
		if len(m.logBuf) > maxLogLines {
			m.logBuf = m.logBuf[len(m.logBuf)-maxLogLines:]
		}
		m.view.SetContent(strings.Join(m.logBuf, "\n"))
		m.view.GotoBottom()
		return m, m.waitForLine()

	case msgStepBeg:
		i := int(msg)
		if i >= 0 && i < len(m.steps) {
			m.steps[i].Status = statusRunning
		}
		return m, m.waitForLine()

	case msgStepEnd:
		if msg.idx >= 0 && msg.idx < len(m.steps) {
			m.steps[msg.idx].Status = msg.status
		}
		// Promote the next pending step (visual hint that something's about to start).
		for i := msg.idx + 1; i < len(m.steps); i++ {
			if m.steps[i].Status == statusPending {
				m.steps[i].Status = statusRunning
				break
			}
		}
		return m, m.waitForLine()

	case msgFinished:
		m.finished = true
		m.finalErr = msg.err
	}

	var cmd tea.Cmd
	m.view, cmd = m.view.Update(msg)
	return m, cmd
}

// Style helpers.
var (
	styleTitle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7c5cff"))
	styleOK       = lipgloss.NewStyle().Foreground(lipgloss.Color("#5cffd1"))
	styleErr      = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5c7c"))
	styleRun      = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffd166"))
	stylePending  = lipgloss.NewStyle().Foreground(lipgloss.Color("#666"))
	styleHelp     = lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Italic(true)
	styleViewport = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#444")).Padding(0, 1)
)

func (m *runModel) View() string {
	var sb strings.Builder

	sb.WriteString(styleTitle.Render(fmt.Sprintf("LaraEnv deploy-tui — %s @ %s", m.cfg.Name, m.host.Address)))
	sb.WriteString("\n\n")

	// Progress bar: gradient bar + done/total + current step caption.
	frac := m.progressFraction()
	doneN := int(frac * float64(len(m.steps)))
	pct := int(frac * 100)
	caption := m.currentStepTitle()
	if m.finished {
		if m.finalErr != nil {
			caption = "failed — see log below"
		} else {
			caption = "done"
		}
	}
	sb.WriteString("  ")
	sb.WriteString(m.prog.ViewAs(frac))
	sb.WriteString(fmt.Sprintf("  %d%%  (%d/%d)", pct, doneN, len(m.steps)))
	sb.WriteString("\n  ")
	sb.WriteString(styleHelp.Render(caption))
	sb.WriteString("\n\n")

	for _, st := range m.steps {
		var icon, line string
		switch st.Status {
		case statusPending:
			icon = "○"
			line = stylePending.Render(fmt.Sprintf(" %s  %s", icon, st.Title))
		case statusRunning:
			icon = "⏵"
			line = styleRun.Render(fmt.Sprintf(" %s  %s", icon, st.Title))
		case statusDone:
			icon = "✓"
			line = styleOK.Render(fmt.Sprintf(" %s  %s", icon, st.Title))
		case statusError:
			icon = "✗"
			line = styleErr.Render(fmt.Sprintf(" %s  %s", icon, st.Title))
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(styleViewport.Render(m.view.View()))
	sb.WriteString("\n")

	if m.finished {
		if m.finalErr != nil {
			sb.WriteString(styleErr.Render(fmt.Sprintf("\nFAILED after %s: %v", time.Since(m.startedAt).Round(time.Second), m.finalErr)))
		} else {
			eff := m.cfg.EffectiveDomain()
			scheme := "http"
			if m.cfg.EnableSSL {
				scheme = "https"
			}
			sb.WriteString(styleOK.Render(fmt.Sprintf("\n✓ Deployed in %s — %s://%s/", time.Since(m.startedAt).Round(time.Second), scheme, eff)))
		}
		sb.WriteString(styleHelp.Render("\nPress q to exit."))
	} else {
		sb.WriteString(styleHelp.Render("Streaming live log. Ctrl+C aborts (won't roll back already-applied steps)."))
	}

	return sb.String()
}

// runStreaming executes a single command on the SSH client, streaming
// stdout + stderr line-by-line into logCh as they arrive. Returns the
// session's exit error (non-nil if the command exited non-zero or the
// stream copy failed).
func runStreaming(client *xssh.Client, cmd string, logCh chan<- string) error {
	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("open session: %w", err)
	}
	defer sess.Close()

	stdout, err := sess.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}
	if err := sess.Start(cmd); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	var wg sync.WaitGroup
	pump := func(r io.Reader) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			logCh <- sc.Text()
		}
	}
	wg.Add(2)
	go pump(stdout)
	go pump(stderr)

	runErr := sess.Wait()
	wg.Wait()
	return runErr
}

// probeHost runs the detection probe pass against the host and returns the
// result, or nil if anything goes wrong (auth failure, network, etc) — in
// which case BuildPlan falls back to "install everything" which is the
// right behaviour for a clean VPS. Output is silent (printed to the form
// completion screen, not the live TUI) so the user only sees one log
// stream during the actual run.
func probeHost(h config.SSHHost) *deploy.DetectionResult {
	fmt.Print("Probing host… ")
	client, err := dialDirect(h)
	if err != nil {
		fmt.Printf("(connect failed, will assume clean: %v)\n", err)
		return nil
	}
	defer client.Close()
	det, err := deploy.RunDetection(client, func(string) {})
	if err != nil {
		fmt.Printf("(probe failed, will assume clean: %v)\n", err)
		return nil
	}
	fmt.Printf("%s %s, %d packages found\n", det.OSFamily, det.OSVersion, len(det.Installed))
	return det
}

// dialDirect builds a temporary pkgssh.Manager configured for our usage:
// hostDirFunc returns "" so buildAuth's filepath.Join("", h.KeyName) ends
// up being just h.KeyName — which is the absolute key path the user typed
// into the form. For password auth the hostDirFunc is never consulted.
func dialDirect(h config.SSHHost) (*xssh.Client, error) {
	mgr := pkgssh.NewManager("", "", func(string) string { return "" })
	return mgr.Dial(h)
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ---------- main ----------

func main() {
	in, err := collectInput()
	if err != nil {
		fmt.Println("aborted:", err)
		os.Exit(1)
	}

	host := toSSHHost(in)
	cfg := toApplyConfig(in)
	// Probe the host first so the plan only includes work that's actually
	// needed. On a fresh VPS this lists every step; on a re-run it shrinks
	// to just "git pull + rebuild" (or even less). We dial twice — once for
	// detection, once for the runner — but both reuse the same auth context
	// and total handshake cost is ~1-2s.
	det := probeHost(host)
	// deploy-tui has no preset UI — always legacy free-text build script.
	plan := deploy.BuildPlan(cfg, det, cfg.BuildScript, true)

	model := newRunModel(host, cfg, plan)
	prog := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(context.Background()))
	if _, err := prog.Run(); err != nil {
		fmt.Println("tui error:", err)
		os.Exit(1)
	}
	if model.finalErr != nil {
		os.Exit(1)
	}
}
