package pty

import "io"

type Session interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
}

// Waiter is implemented by sessions whose reads do not end on their own when
// the process they host does.
//
// A Windows PTY is a pseudoconsole, and conhost.exe holds the far end of the
// output pipe open after the shell exits — so the read loop that drives
// term:data / term:exit blocks forever and the pane stays on screen, alive
// looking and answering nothing. Waiting on the process handle is the only
// signal that it is over. Unix PTYs and SSH sessions end their own reads and
// deliberately do not implement this.
type Waiter interface {
	// Wait blocks until the hosted process exits.
	Wait() error
}
