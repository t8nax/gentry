package cli

import "strings"

// flags parses the command line of one command. Flags are long only (--json);
// -h and --help anywhere show the command help; "--" ends flags. A value flag
// takes its value as --name=value or as the next argument, unless that argument
// is a long flag or -h. Error texts come from the message catalog, which is why
// the standard flag package is not used.
//
// Only the flags declared in the command spec can be defined, and positional
// arguments beyond the declared ones are refused, so the help always names
// everything a command accepts.
type flags struct {
	cmd     command
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
	c, ok := lookup(cmd)
	if !ok {
		panic("cli: no spec for command " + cmd)
	}
	f := &flags{cmd: c, bools: map[string]*bool{}, strings: map[string]*stringFlag{}}
	// --json is no flag of the command itself: it chooses the form of the
	// output, which emit and fail print (Env.json).
	if c.acceptsJSON() {
		f.bools["json"] = new(bool)
	}
	return f
}

// Bool defines the boolean flag --name declared in the command spec.
func (f *flags) Bool(name string) *bool {
	f.declared(name, false)
	v := new(bool)
	f.bools[name] = v
	return v
}

// String defines the value flag --name declared in the command spec.
func (f *flags) String(name string) *stringFlag {
	f.declared(name, true)
	v := new(stringFlag)
	f.strings[name] = v
	return v
}

// declared panics unless the command spec declares the flag of this kind: an
// undeclared flag would be missing from the help.
func (f *flags) declared(name string, value bool) {
	for _, s := range f.cmd.flags {
		if s.name == name && (s.value != "") == value {
			return
		}
	}
	panic("cli: flag --" + name + " is not declared in the spec of " + f.cmd.name)
}

// onHelp is called when a command shows its help; tests use it to check that
// every declared flag is defined.
var onHelp = func(*flags) {}

// parse parses args. When done is true the command must return code at once:
// the help was shown or the arguments are wrong.
func (f *flags) parse(args []string, env Env) (code int, done bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			f.args = append(f.args, args[i+1:]...)
			return f.checkArgs(env)
		case a == "-h" || a == "--help":
			onHelp(f)
			return commandHelp(f.cmd, env), true
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
				return fail(env, unknownFlag(f.cmd.name, "--"+name)), true
			}
			if hasValue {
				return fail(env, flagValue("--"+name)), true
			}
			*v = true
		case strings.HasPrefix(a, "-") && a != "-":
			return fail(env, unknownFlag(f.cmd.name, a)), true
		default:
			f.args = append(f.args, a)
		}
	}
	return f.checkArgs(env)
}

// checkArgs refuses positional arguments beyond the declared ones.
func (f *flags) checkArgs(env Env) (code int, done bool) {
	n := len(f.cmd.args)
	switch {
	case len(f.args) <= n, n > 0 && f.cmd.args[n-1].many:
		return 0, false
	case n == 0:
		return fail(env, unexpectedArgs(f.cmd.name)), true
	default:
		return fail(env, extraArgs(f.cmd.name, f.args[n:])), true
	}
}
