// Package cli holds the one-shot maintenance commands of the mirror-backend
// binary.
//
// They live here rather than inside main() so that main stays a flat list of
// initialisation steps: main never asks "is this a command or a server?", it
// only asks "which command was requested, and what does it need?".
package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"mirror/internal/infra/config"
	"mirror/internal/infra/logger"
	"mirror/internal/proxyconfig"
	servicev1 "mirror/internal/service/v1"

	"go.uber.org/zap"
)

// unsetArgument marks an optional flag argument that was not supplied. It must
// be a value a user cannot type on a command line.
const unsetArgument = "\x00unset"

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

	// Optional means the argument may be omitted, in which case the effective
	// default (shown in -h) is used.
	Optional bool

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
			Flag: "dump-cache-config",
			Usage: "write the caching proxy configuration (routes derived from mirror_list) " +
				"to this path, then exit. The path is optional when mirror.cache_config is set",
			TakesValue: true,
			Optional:   true,
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

// Parse reads the command line.
//
// It returns (nil, nil) when no command was given, in which case the caller
// starts the server, and a non-nil error when the command line itself was
// wrong — a typo must never fall through to a server start.
//
// The caller must initialise the dependencies named by Needs before calling Run.
func Parse() (*Command, error) {
	commands := Commands()

	// A private flag set with ContinueOnError: the global one exits the process
	// on a bad flag, which would skip the reporting below (and looks exactly
	// like a crash with no message).
	set := flag.NewFlagSet("mirror-backend", flag.ContinueOnError)
	set.Usage = func() { fmt.Fprint(set.Output(), Usage()) }

	// Bind every command's flag in one pass, so adding a command never means
	// touching the parsing code.
	values := make(map[string]*string, len(commands))
	switches := make(map[string]*bool, len(commands))

	for i := range commands {
		command := &commands[i]

		if command.TakesValue {
			def := ""
			if command.Optional {
				// An argument-less "-flag" is eaten by the flag package as an
				// empty value, so the default has to be a value no user can
				// type. It is replaced below with the configured default (if
				// any), and anything still unset is reported as an error by the
				// command instead of silently starting the server.
				def = unsetArgument
			}
			values[command.Flag] = set.String(command.Flag, def, command.Usage)
			continue
		}

		switches[command.Flag] = set.Bool(command.Flag, false, command.Usage)
	}

	if err := set.Parse(os.Args[1:]); err != nil {
		return nil, err
	}

	for i := range commands {
		command := &commands[i]

		if command.Optional {
			if !flagWasGiven(set, command.Flag) {
				continue
			}

			value := strings.TrimSpace(*values[command.Flag])
			switch {
			case value == unsetArgument:
				// Present without an argument: fall back to the configured path,
				// and let the command report the problem when that is empty too.
				value = strings.TrimSpace(config.Get().Mirror.CacheConfig)
			case strings.HasPrefix(value, "-") && value != "-":
				// The flag package consumed the next argument, so
				// "-dump-cache-config -h" would otherwise be read as a path and
				// write a file literally named "-h".
				return nil, fmt.Errorf(
					"-%s: %q looks like another flag, not a path", command.Flag, value)
			}

			command.value = value
			return command, nil
		}

		if command.TakesValue {
			// An empty value means "not requested"; "-" is a real value that
			// asks for stdout.
			if value := strings.TrimSpace(*values[command.Flag]); value != "" {
				command.value = value
				return command, nil
			}
			continue
		}

		if *switches[command.Flag] {
			return command, nil
		}
	}

	return nil, nil
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

// Usage returns the text printed for -h.
//
// The flag package would print the internal placeholder used for optional
// arguments, so the defaults are rendered here instead: the configured path for
// an optional argument, and nothing for the others.
func Usage() string {
	var builder strings.Builder

	builder.WriteString("Usage of mirror-backend:\n")

	for _, command := range Commands() {
		if command.TakesValue {
			builder.WriteString("  -" + command.Flag + " <path>\n")

			def := strings.TrimSpace(config.Get().Mirror.CacheConfig)
			if def == "" {
				def = "(required: mirror.cache_config is not set; use - for stdout)"
			}
			builder.WriteString("    \t" + command.Usage + "\n")
			builder.WriteString("    \t(default " + strconv.Quote(def) + ")\n")
			continue
		}

		builder.WriteString("  -" + command.Flag + "\n")
		builder.WriteString("    \t" + command.Usage + "\n")
	}

	builder.WriteString("\nWith no command the server starts.\n")

	return builder.String()
}

// Run performs the command and reports the outcome. Every command is a one-shot
// action: after it returns, the caller stops.
func (c Command) Run() error {
	logger.L.Info("running maintenance command", zap.String("command", c.Flag))

	return c.run(c.Argument())
}

// flagWasGiven reports whether a flag appeared on the command line, which is
// how an optional-value flag with an empty effective default is detected.
func flagWasGiven(set *flag.FlagSet, name string) bool {
	given := false

	set.Visit(func(f *flag.Flag) {
		if f.Name == name {
			given = true
		}
	})

	return given
}

// dumpCacheConfig renders the caching proxy's route table from the mirror list.
//
// This is what keeps the two in step: the proxy routes mirror directories to
// upstreams, and hand-maintaining that list means a newly added mirror answers
// 404 until someone remembers to edit a second file.
func dumpCacheConfig(path string) error {
	if path == "" {
		return errors.New("no output path: pass one (-dump-cache-config <path>, or - for stdout), or set mirror.cache_config")
	}

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
