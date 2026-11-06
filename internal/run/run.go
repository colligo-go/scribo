package run

import (
	"bytes"
	"encoding/json"
	"os"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/colligo-go/scribo/internal/db"
)

const unknown = "unknown"

type argvShape struct {
	Command string   `json:"command"`
	Flags   []string `json:"flags"`
	Args    int      `json:"args"`
}

func Meta(argvShape, step string) db.RunMeta {
	host, err := os.Hostname()
	if err != nil {
		host = ""
	}
	toolVersion, gitSHA := buildDetails()
	return db.RunMeta{
		ToolVersion: toolVersion,
		GitSHA:      gitSHA,
		GoVersion:   runtime.Version(),
		ArgvShape:   argvShape,
		Step:        step,
		Host:        host,
	}
}

func buildDetails() (toolVersion, gitSHA string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return unknown, unknown
	}
	toolVersion = info.Main.Version
	if toolVersion == "" {
		toolVersion = unknown
	}

	gitSHA = unknown
	modified := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if setting.Value != "" {
				gitSHA = setting.Value
			}
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if gitSHA != unknown && modified {
		gitSHA += "-dirty"
	}
	return toolVersion, gitSHA
}

func ArgvShape(commandPath string, flagNames []string, positional int) string {
	flags := slices.Clone(flagNames)
	slices.Sort(flags)
	flags = slices.Compact(flags)
	if flags == nil {
		flags = []string{}
	}

	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.Encode(argvShape{Command: commandPath, Flags: flags, Args: positional})
	return strings.TrimSuffix(buffer.String(), "\n")
}
