package cli

import "strings"

// flags parses the flags of one command. Flags are long only (--json);
// -h and --help anywhere show the help; "--" ends flags. A value flag takes
// its value as --name=value or as the next argument, unless that argument is
// a long flag or -h. Error texts come from the message catalog, which is why the
// standard flag package is not used.
type flags struct {
	cmd     string
	bools   map[string]*bool
	strings map[string]*stringFlag
	args    []string // positional arguments after parsing
}

// stringFlag is the value of a value flag.
type stringFlag struct {
	Value string
	Set   bool // the flag is given
}

func newFlags(cmd string) *flags {
	return &flags{cmd: cmd, bools: map[string]*bool{}, strings: map[string]*stringFlag{}}
}

// Bool defines a boolean flag --name.
func (f *flags) Bool(name string) *bool {
	v := new(bool)
	f.bools[name] = v
	return v
}

// String defines a value flag --name.
func (f *flags) String(name string) *stringFlag {
	v := new(stringFlag)
	f.strings[name] = v
	return v
}

// parse parses args. When done is true the command must return code at once:
// the help was shown or the arguments are wrong.
func (f *flags) parse(args []string, env Env) (code int, done bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			f.args = append(f.args, args[i+1:]...)
			return 0, false
		case a == "-h" || a == "--help":
			return runHelp(nil, env), true
		case strings.HasPrefix(a, "--"):
			name, value, hasValue := strings.Cut(a[2:], "=")
			if v, ok := f.strings[name]; ok {
				if !hasValue {
					if i+1 == len(args) || strings.HasPrefix(args[i+1], "--") || args[i+1] == "-h" {
						return fail(env, flagValueMissing("--"+name)), true
					}
					i++
					value = args[i]
				}
				v.Value, v.Set = value, true
				continue
			}
			v, ok := f.bools[name]
			if !ok {
				return fail(env, unknownFlag(f.cmd, "--"+name)), true
			}
			if hasValue {
				return fail(env, flagValue("--"+name)), true
			}
			*v = true
		case strings.HasPrefix(a, "-") && a != "-":
			return fail(env, unknownFlag(f.cmd, a)), true
		default:
			f.args = append(f.args, a)
		}
	}
	return 0, false
}
