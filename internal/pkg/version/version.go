// Package version contains everything related to versioning of the credential
// provider.
package version

import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"text/tabwriter"

	json "github.com/json-iterator/go"
)

// Version is the version of the build.
const Version = "0.1.0"

// Variables injected during build-time.
var buildDate string // build date in ISO8601 format, output of $(date -u +'%Y-%m-%dT%H:%M:%SZ')

// Info contains all the version information.
type Info struct {
	Version       string   `json:"version,omitempty"`
	GitCommit     string   `json:"gitCommit,omitempty"`
	GitCommitDate string   `json:"gitCommitDate,omitempty"`
	BuildDate     string   `json:"buildDate,omitempty"`
	GoVersion     string   `json:"goVersion,omitempty"`
	Compiler      string   `json:"compiler,omitempty"`
	Platform      string   `json:"platform,omitempty"`
	LDFlags       string   `json:"ldFlags,omitempty"`
	Dependencies  []string `json:"dependencies,omitempty"`
}

// Get returns a new version info instance.
func Get() (*Info, error) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return nil, errors.New("unable to retrieve build info")
	}

	const unknown = "unknown"

	gitCommit := unknown
	gitCommitDate := unknown
	ldFlags := unknown

	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			gitCommit = s.Value

		case "vcs.time":
			gitCommitDate = s.Value

		case "-ldflags":
			ldFlags = s.Value
		}
	}

	dependencies := []string{}

	for _, d := range info.Deps {
		dependencies = append(
			dependencies,
			fmt.Sprintf("%s %s %s", d.Path, d.Version, d.Sum),
		)
	}

	return &Info{
		Version:       Version,
		GitCommit:     gitCommit,
		GitCommitDate: gitCommitDate,
		BuildDate:     buildDate,
		GoVersion:     runtime.Version(),
		Compiler:      runtime.Compiler,
		Platform:      fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		LDFlags:       ldFlags,
		Dependencies:  dependencies,
	}, nil
}

// String returns the string representation of the version info.
func (i *Info) String() string {
	b := strings.Builder{}
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)

	v := reflect.ValueOf(*i)

	t := v.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		value := v.FieldByName(field.Name)

		valueString := ""
		isMultiLineValue := false

		switch field.Type.Kind() { //nolint:exhaustive
		case reflect.Slice:
			// Only expecting []string here; ignore other slices.
			if s, ok := value.Interface().([]string); ok {
				const sep = "\n  "

				valueString = sep + strings.Join(s, sep)
			}

			isMultiLineValue = true

		case reflect.String:
			valueString = value.String()
		}

		if strings.TrimSpace(valueString) != "" {
			fmt.Fprintf(w, "%s:", field.Name) //nolint:errcheck

			if isMultiLineValue {
				fmt.Fprint(w, valueString) //nolint:errcheck
			} else {
				fmt.Fprintf(w, "\t%s", valueString) //nolint:errcheck
			}

			if i+1 < t.NumField() {
				fmt.Fprintf(w, "\n") //nolint:errcheck
			}
		}
	}

	w.Flush() //nolint:errcheck

	return b.String()
}

// JSONString returns the JSON representation of the version info.
func (i *Info) JSONString() (string, error) {
	b, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal JSON string: %w", err)
	}

	return string(b), nil
}
