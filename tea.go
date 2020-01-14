package tea

import (
	"fmt"
	"io"
	"strings"

	"github.com/pkg/term"
)

// Escape sequence
const esc = "\033["

// Msg represents an action. It's used by Update to update the UI.
type Msg interface{}

// Model contains the data for an application
type Model interface{}

// Cmd is an IO operation. If it's nil it's considered a no-op.
type Cmd func() Msg

// Sub is an event subscription. If it's nil it's considered a no-op. But why
// would you subscribe to nil?
type Sub func(Model) Msg

// Update is called when a message is received. It may update the model and/or
// send a command.
type Update func(Msg, Model) (Model, Cmd)

// View produces a string which will be rendered to the terminal
type View func(Model) string

// Program is a terminal user interface
type Program struct {
	model         Model
	update        Update
	view          View
	subscriptions []Sub
	rw            io.ReadWriter
}

// Quit command
func Quit() Msg {
	return quitMsg{}
}

// Signals that the program should quit
type quitMsg struct{}

// NewProgram creates a new Program
func NewProgram(model Model, update Update, view View, subs []Sub) *Program {
	return &Program{
		model:         model,
		update:        update,
		view:          view,
		subscriptions: subs,
	}
}

// Start initializes the program
// TODO: error channel
func (p *Program) Start() error {
	var (
		model = p.model
		cmd   Cmd
		cmds  = make(chan Cmd)
		msgs  = make(chan Msg)
		done  = make(chan struct{})
	)

	tty, err := term.Open("/dev/tty")
	if err != nil {
		return err
	}

	p.rw = tty
	tty.SetRaw()
	defer func() {
		showCursor()
		tty.Restore()
	}()

	// Render initial view
	hideCursor()
	p.render(model, true)

	// Subscribe to user input
	// TODO: should we move this to the end-user program level or just keep this
	// here, since it blocks nicely and user input will probably be something
	// users typically need?
	go func() {
		for {
			select {
			default:
				msg, _ := ReadKey(p.rw)
				msgs <- KeyPressMsg(msg)
			}
		}
	}()

	// Process commands
	go func() {
		for {
			select {
			case <-done:
				return
			case cmd := <-cmds:
				if cmd != nil {
					go func() {
						msgs <- cmd()
					}()
				}
			}
		}
	}()

	// Subscriptions. Subscribe to user input.
	// TODO: should the blocking `for` be here, or in the end-user portion
	// of the program?
	go func() {
		select {
		case <-done:
			return
		default:
			if len(p.subscriptions) > 0 {
				for _, sub := range p.subscriptions {
					if sub != nil {
						go func() {
							for {
								msgs <- sub(p.model)
							}
						}()
					}
				}
			}
		}
	}()

	// Handle updates and draw
	for {
		select {
		case msg := <-msgs:
			if _, ok := msg.(quitMsg); ok {
				close(done)
				return nil
			}

			model, cmd = p.update(msg, model)
			cmds <- cmd // process command (if any)
			p.render(model, false)
			p.model = model
		}
	}
}

// Render a view
func (p *Program) render(model Model, init bool) {
	view := p.view(model)

	// We need to add carriage returns to ensure that the cursor travels to the
	// start of a column after a newline
	view = strings.Replace(view, "\n", "\r\n", -1)

	if !init {
		clearLines(strings.Count(view, "\r\n"))
	}
	io.WriteString(p.rw, view)
}

func hideCursor() {
	fmt.Printf(esc + "?25l")
}

func showCursor() {
	fmt.Printf(esc + "?25h")
}

func cursorUp(n int) {
	fmt.Printf(esc+"%dF", n)
}

func clearLine() {
	fmt.Printf(esc + "2K")
}

func clearLines(n int) {
	for i := 0; i < n; i++ {
		cursorUp(1)
		clearLine()
	}
}

// ClearScreen clears the visible portion of the terminal
func ClearScreen() {
	fmt.Printf(esc + "2J" + esc + "3J" + esc + "1;1H")
}