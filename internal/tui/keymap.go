package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all key bindings for the TUI.
// Implements the help.KeyMap interface for the help overlay.
type KeyMap struct {
	Spawn      key.Binding // s — spawn agent (default CLI)
	SpawnWith  key.Binding // S — spawn agent (pick CLI)
	Terminate  key.Binding // t — terminate agent
	Pause      key.Binding // p — pause system
	Resume     key.Binding // r — resume system
	AddTask    key.Binding // a — add task (Huh form)
	Checkpoint key.Binding // c — sprint checkpoint
	Yolo       key.Binding // y — toggle auto-resume
	Help       key.Binding // ? — toggle help overlay
	Quit       key.Binding // q — quit TUI (system continues)
	Stop       key.Binding // Q — stop system then quit
}

// NewKeyMap returns a KeyMap with the default key bindings from the spec.
func NewKeyMap() KeyMap {
	return KeyMap{
		Spawn: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "spawn (default cli)"),
		),
		SpawnWith: key.NewBinding(
			key.WithKeys("S"),
			key.WithHelp("S", "spawn (pick cli)"),
		),
		Terminate: key.NewBinding(
			key.WithKeys("t"),
			key.WithHelp("t", "terminate"),
		),
		Pause: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "pause"),
		),
		Resume: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "resume"),
		),
		AddTask: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "add"),
		),
		Checkpoint: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "checkpoint"),
		),
		Yolo: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "yolo"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", "quit"),
		),
		Stop: key.NewBinding(
			key.WithKeys("Q"),
			key.WithHelp("Q", "stop"),
		),
	}
}

// ShortHelp returns the key bindings shown in the footer bar.
// Order matches spec §Footer Bar: s, p, r, a, c, y, ?, q, Q.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Spawn,
		k.SpawnWith,
		k.Terminate,
		k.Pause,
		k.Resume,
		k.AddTask,
		k.Checkpoint,
		k.Yolo,
		k.Help,
		k.Quit,
		k.Stop,
	}
}

// FullHelp returns grouped key bindings for the help overlay.
// Groups: actions (spawn, pause, resume, checkpoint), system (quit, stop, help), navigation (add task).
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		// Actions: commands that affect the running system
		{k.Spawn, k.SpawnWith, k.Terminate, k.Pause, k.Resume, k.Checkpoint, k.Yolo},
		// System: TUI lifecycle and help
		{k.Quit, k.Stop, k.Help},
		// Navigation: task management
		{k.AddTask},
	}
}
