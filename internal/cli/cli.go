// Package cli holds the one-shot maintenance commands of the mirror-backend
// binary.
//
// They live here rather than inside main() so that main stays a flat list of
// initialisation steps: main never asks "is this a command or a server?", it
// only asks "which command was requested, and what does it need?".
package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"mirror/internal/infra/logger"
	"mirror/internal/proxyconfig"
	servicev1 "mirror/internal/service/v1"

	"go.uber.org/zap"
)

// Dependency names something a command needs initialised before it can run.
type Dependency int

const (
	// NeedsDatabase means the command reads the mirror list.
	NeedsDatabase Dependency = iota

	// NeedsCache means the command talks to Redis.
	NeedsCache
)

// Command is one maintenance action available from the command line.
type Command struct {
	// Flag is the command line flag that triggers this command.
	Flag string

	// Usage is the one-line help text shown by -h.
	Usage string

	// TakesValue is true when the flag carries an argument.
	TakesValue bool

	// Needs lists the dependencies to initialise before Run is called.
	Needs []Dependency

	// value is the flag argument, filled in by Parse.
	value string

	// run performs the command. It is only called when the flag was given.
	run func(value string) error
}

// Commands lists every maintenance action, in help order.
//
// Adding one means adding an entry here: main reads Needs, so nothing else in
// the program changes, and no command has to initialise a datastore it does not
// use — dropping the cache needs Redis, generating the proxy configuration does
// not and works even when Redis is down.
func Commands() []Command {
	return []Command{
		{
			Flag:       "dump-cache-config",
			Usage:      "write the caching proxy configuration (routes derived from mirror_list) to this file, then exit",
			TakesValue: true,
			Needs:      []Dependency{NeedsDatabase},
			run:        dumpCacheConfig,
		},
		{
			Flag:  "drop-caches",
			Usage: "drop the cached mirror list and sync status so edits to mirror_list take effect immediately, then exit",
			Needs: []Dependency{NeedsDatabase, NeedsCache},
			run:   dropCaches,
		},
	}
}

// Parse reads the command line and returns the requested command, or nil when
// none was given (in which case the caller starts the server).
//
// The caller must initialise the dependencies named by Needs before calling Run.
func Parse() *Command {
	commands := Commands()

	// Bind every command's flag in one pass, so adding a command never means
	// touching the parsing code.
	values := make(map[string]*string, len(commands))
	switches := make(map[string]*bool, len(commands))

	for i := range commands {
		command := &commands[i]

		if command.TakesValue {
			values[command.Flag] = flag.String(command.Flag, "", command.Usage)
			continue
		}

		switches[command.Flag] = flag.Bool(command.Flag, false, command.Usage)
	}

	flag.Parse()

	for i := range commands {
		command := &commands[i]

		if command.TakesValue {
			// An empty value means "not requested"; "-" is a real value that
			// asks for stdout.
			if value := strings.TrimSpace(*values[command.Flag]); value != "" {
				command.value = value
				return command
			}
			continue
		}

		if *switches[command.Flag] {
			return command
		}
	}

	return nil
}

// Argument returns the flag value the command was invoked with.
func (c Command) Argument() string {
	return c.value
}

// Requires reports whether the command needs a dependency initialised.
func (c Command) Requires(dependency Dependency) bool {
	for _, needed := range c.Needs {
		if needed == dependency {
			return true
		}
	}

	return false
}

// Run performs the command and reports the outcome. Every command is a one-shot
// action: after it returns, the caller stops.
func (c Command) Run() error {
	logger.L.Info("running maintenance command", zap.String("command", c.Flag))

	return c.run(c.Argument())
}

// dumpCacheConfig renders the caching proxy's route table from the mirror list.
//
// This is what keeps the two in step: the proxy routes mirror directories to
// upstreams, and hand-maintaining that list means a newly added mirror answers
// 404 until someone remembers to edit a second file.
func dumpCacheConfig(path string) error {
	if err := proxyconfig.Write(path); err != nil {
		return err
	}

	if path != "-" {
		fmt.Fprintf(os.Stdout, "wrote %s\n", path)
	}

	return nil
}

// dropCaches clears the cached mirror list and sync snapshot.
//
// Editing mirror_list by hand does not notify the running process, and the list
// is cached for a day, so an edit is otherwise invisible until the TTL expires.
func dropCaches(_ string) error {
	if err := servicev1.DropCaches(); err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, "caches dropped")

	return nil
}
