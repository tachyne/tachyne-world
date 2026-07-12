package plugin

// Command is a chat command a plugin registers at Enable. Run executes
// on the tick goroutine.
type Command struct {
	Name    string // without the leading slash
	Usage   string // one-line arg synopsis, e.g. "<radius> [amount]"
	Help    string // one-line description for /help
	Aliases []string
	OpOnly  bool
	Run     func(ctx CommandContext)
}

// CommandContext is passed to a running command handler.
type CommandContext interface {
	// Sender is the invoking player.
	Sender() Player
	// Args are the whitespace-split arguments after the command name.
	Args() []string
	// Reply sends a private message to the sender.
	Reply(text string)
	Server() Server
}
