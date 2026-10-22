package i18n

import "sync/atomic"

// global holds the catalogue set loaded at startup (main.go calls
// App.LoadCatalogues, which calls SetGlobal). Backend packages that construct
// an *Error reach it directly instead of threading a *Set through 25
// packages that have no business knowing the UI's language — internal/ssh
// and internal/cron, for instance, would otherwise gain a dependency on
// something that is purely a presentation concern.
//
// It is an atomic.Pointer, not a plain exported var, because it is read from
// every goroutine that can construct an *Error (which is most of them) and
// nothing about that reader ever held a lock — a plain *Set here was only
// safe by accident, on the assumption that main.go's single write always
// happens-before every read. A catalogue hot-reload, a second App, or a
// fan-out test assigning it off the main goroutine would have been an
// unguarded data race across 25 packages.
//
// It is nil until SetGlobal runs, and stays nil in any unit test that never
// calls it. See (*Error).Error for how that is handled.
var global atomic.Pointer[Set]

// SetGlobal records the loaded catalogues for every *Error and i18n.T call to
// render against. Called once, by App.LoadCatalogues at startup.
func SetGlobal(s *Set) { global.Store(s) }

// Global is the catalogue set SetGlobal last recorded, or nil if it was never
// called.
func Global() *Set { return global.Load() }

// active holds the BCP-47 code errors render in right now. It changes far
// less often than it is read (every (*Error).Error call, from any
// goroutine), so an atomic.Value beats a mutex here.
var active atomic.Value

func init() {
	active.Store(Fallback)
}

// SetActive records the language errors constructed anywhere in the backend
// should render in. It must be called wherever the active language is
// established or changes: LoadCatalogues at startup, SetLanguage, and
// watchLanguage's callback in app.go. Missing one of those means an error
// renders in whatever language was active last, not the one the window
// is actually showing.
func SetActive(code string) {
	if code == "" {
		code = Fallback
	}
	active.Store(code)
}

// Active is the language (*Error).Error renders in.
func Active() string {
	if v, ok := active.Load().(string); ok && v != "" {
		return v
	}
	return Fallback
}

// T renders a catalogue key in the active language, the same as calling
// Global().T(Active(), key, vars) — except it degrades to the bare key path
// instead of panicking when Global() is nil. Backend packages that emit
// translatable prose that is not itself an error (a notification's title, a
// progress message) call this instead of reaching into Global() directly, so
// every one of them gets the same nil-Global guard without repeating it.
func T(key string, vars map[string]any) string {
	set := Global()
	if set == nil {
		return key
	}
	return set.T(Active(), key, vars)
}

// Error is a translatable error: it carries a catalogue key and its
// interpolation vars instead of a fixed English sentence, so the same error
// value renders in whatever language is active at the moment it is
// displayed — which is what a long-lived error (one stored, logged, then
// shown to the user later) actually needs, rather than the language that was
// active when it was constructed.
//
// Err, when set, is a wrapped error the way fmt.Errorf's %w works — but it is
// never itself translated. It always comes from the standard library or the
// OS (a syscall error, a JSON decode error), and translating library prose
// is not this mechanism's job.
type Error struct {
	Key  string
	Vars map[string]any
	Err  error
}

// Errorf builds a translatable error from a catalogue key and its vars.
func Errorf(key string, vars map[string]any) error {
	return &Error{Key: key, Vars: vars}
}

// Wrapf builds a translatable error that also wraps err, the way
// fmt.Errorf("%s: %w", ...) would. Use it when the underlying error (I/O,
// exec, JSON) needs to keep flowing through errors.Is/errors.As.
//
// Wrapf(nil, ...) returns nil, the same way fmt.Errorf("%w", nil) would
// still build a non-nil error but callers of Wrapf almost always write
// `if err != nil { return i18n.Wrapf(err, ...) }` — so a nil err reaching
// here is nearly always a call that should have been skipped, and building
// a non-nil *Error out of a nil cause is a footgun a fan-out task converting
// dozens of these would eventually trip on.
func Wrapf(err error, key string, vars map[string]any) error {
	if err == nil {
		return nil
	}
	return &Error{Key: key, Vars: vars, Err: err}
}

// Error renders the key in the active language, appending the wrapped error
// exactly the way fmt.Errorf("%s: %w", ...) would when Err is set.
//
// If Global() is nil — a package used before LoadCatalogues runs, or a unit
// test that never loads catalogues — this falls back to the bare key path
// rather than panicking. That mirrors Set.T's own behaviour for a key
// missing from every catalogue (fallback included): an unmistakable,
// debuggable string beats a crash or a guess at English prose it has no way
// to look up.
func (e *Error) Error() string {
	msg := T(e.Key, e.Vars)
	if e.Err != nil {
		msg = msg + ": " + e.Err.Error()
	}
	return msg
}

// Unwrap exposes the wrapped error so errors.Is and errors.As keep working
// across every error this package produces.
func (e *Error) Unwrap() error { return e.Err }
