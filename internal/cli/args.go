package cli

import (
	"fmt"
	"strings"
)

// cliArgs is a tiny argument parser: value flags (`--name v` or `--name=v`)
// and boolean flags are recognized anywhere, everything else is positional.
// The standard flag package stops at the first positional argument, which
// makes natural CLI forms like `tasks start <agent> <session> input --wait`
// impossible to express.
type cliArgs struct {
	positional []string
	values     map[string]string
	flags      map[string]bool
}

// parseCLI splits args. boolNames lists the flags that take no value; any
// other `--name` consumes the next argument as its value.
func parseCLI(args []string, boolNames ...string) (*cliArgs, error) {
	bools := map[string]bool{}
	for _, name := range boolNames {
		bools[name] = true
	}
	result := &cliArgs{values: map[string]string{}, flags: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") || arg == "--" {
			result.positional = append(result.positional, arg)
			continue
		}
		name := strings.TrimPrefix(arg, "--")
		if bools[name] {
			result.flags[name] = true
			continue
		}
		if eq := strings.Index(name, "="); eq >= 0 {
			result.values[name[:eq]] = name[eq+1:]
			continue
		}
		if i+1 >= len(args) {
			return nil, fmt.Errorf("选项 --%s 缺少值", name)
		}
		result.values[name] = args[i+1]
		i++
	}
	return result, nil
}

func (a *cliArgs) arg(index int) string {
	if index < len(a.positional) {
		return a.positional[index]
	}
	return ""
}

func (a *cliArgs) value(name string) string {
	return strings.TrimSpace(a.values[name])
}
