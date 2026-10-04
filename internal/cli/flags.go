package cli

import (
	"strings"

	"github.com/t8nax/gentry/internal/msg"
)

// flags parses the flags of one command. Flags are long only (--json);
// -h and --help anywhere show the help; "--" ends flags. Error texts come
// from the message catalog, which is why the standard flag package is not used.
type flags struct {
	cmd   string
	bools map[string]*bool
	args  []string // positional arguments after parsing
}

func newFlags(cmd string) *flags {
	return &flags{cmd: cmd, bools: map[string]*bool{}}
}

// Bool defines a boolean flag --name.
func (f *flags) Bool(name string) *bool {
	v := new(bool)
	f.bools[name] = v
	return v
}

// parse parses args. When done is true the command must return code at once:
// the help was shown or the arguments are wrong.
func (f *flags) parse(args []string, env Env) (code int, done bool) {
	for i, a := range args {
		switch {
		case a == "--":
			f.args = append(f.args, args[i+1:]...)
			return 0, false
		case a == "-h" || a == "--help":
			return runHelp(nil, env), true
		case strings.HasPrefix(a, "--"):
			name, _, hasValue := strings.Cut(a[2:], "=")
			v, ok := f.bools[name]
			if !ok {
				return usageError(env, msg.Text(msg.ErrUnknownFlag, f.cmd, "--"+name)), true
			}
			if hasValue {
				return usageError(env, msg.Text(msg.ErrFlagValue, "--"+name)), true
			}
			*v = true
		case strings.HasPrefix(a, "-") && a != "-":
			return usageError(env, msg.Text(msg.ErrUnknownFlag, f.cmd, a)), true
		default:
			f.args = append(f.args, a)
		}
	}
	return 0, false
}
