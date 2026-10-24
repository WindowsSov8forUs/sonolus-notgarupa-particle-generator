// Package cli holds the flags and helpers every generator command shares.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/WindowsSov8forUs/sonolus-pack-go/packer"
)

type Common struct {
	Out     string
	Cache   string
	Offline bool
	Verbose bool
	Version bool
	Workers int
}

// Register adds the shared flags to fs.
func (c *Common) Register(fs *flag.FlagSet) {
	fs.StringVar(&c.Out, "out", "out", "output directory: <out>/source (sonolus-pack source), <out>/pack (servable pack), <out>/contracts")
	fs.StringVar(&c.Cache, "cache", ".cache", "download cache directory")
	fs.BoolVar(&c.Offline, "offline", false, "only use cached downloads")
	fs.BoolVar(&c.Verbose, "v", false, "log downloads and items")
	fs.BoolVar(&c.Version, "version", false, "print the version and exit")
	fs.IntVar(&c.Workers, "j", 0, "parallel conversions (default: number of CPUs)")
}

// Logf returns the download logger (nil unless -v).
func (c *Common) Logf() func(string, ...any) {
	if !c.Verbose {
		return nil
	}
	return func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) }
}

type logger struct{}

func (logger) Info(args ...any) { fmt.Fprintln(os.Stderr, append([]any{"[pack]"}, args...)...) }
func (logger) Warning(args ...any) {
	fmt.Fprintln(os.Stderr, append([]any{"[pack][warn]"}, args...)...)
}

// Pack runs sonolus-pack-go on a source tree and lists the items of the
// pack's db.json in order (the generator's order, the original master's);
// sonolus-pack lists them by directory name.
func Pack(source, output string, order []string) error {
	if err := packer.Pack(context.Background(), packer.Options{Input: source, Output: output, Logger: logger{}}); err != nil {
		return err
	}
	return orderDB(filepath.Join(output, "db.json"), order)
}

// orderDB sorts every item list of a db.json by the position of the item
// name in order (names not in order keep their relative order, last). The
// top-level keys and the item JSON stay as they are.
func orderDB(path string, order []string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	rank := make(map[string]int, len(order))
	for i, name := range order {
		rank[name] = i
	}
	pos := func(item json.RawMessage) int {
		var it struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(item, &it) == nil {
			if i, ok := rank[it.Name]; ok {
				return i
			}
		}
		return len(order)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return fmt.Errorf("%s: not a JSON object", path)
	}
	var out bytes.Buffer
	out.WriteByte('{')
	for first := true; dec.More(); first = false {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		key, _ := json.Marshal(tok)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		var items []json.RawMessage
		if json.Unmarshal(value, &items) == nil && len(items) > 1 {
			sort.SliceStable(items, func(i, j int) bool { return pos(items[i]) < pos(items[j]) })
			var list bytes.Buffer
			list.WriteByte('[')
			for i, item := range items {
				if i > 0 {
					list.WriteByte(',')
				}
				list.Write(item)
			}
			list.WriteByte(']')
			value = list.Bytes()
		}
		if !first {
			out.WriteByte(',')
		}
		out.Write(key)
		out.WriteByte(':')
		out.Write(value)
	}
	out.WriteByte('}')
	return os.WriteFile(path, out.Bytes(), 0o644)
}

// WriteJSON writes v as indented JSON, creating the parent directory.
func WriteJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Fatal prints an error and exits.
func Fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

// AxesFile is the file in a pack directory that lists the live settings of
// the pack's items for a server's combination search:
// {"<item type>": [{"name": …, "axes": [{"axis", "bundle", "name"}, …]}, …]}.
const AxesFile = "garupa-axes.json"

// ItemAxes are one item's live settings.
type ItemAxes struct {
	Name string `json:"name"`
	Axes any    `json:"axes"`
}

// WriteAxes writes AxesFile for the items of one type into a pack directory,
// in order (the pack's order; items arrive in completion order).
func WriteAxes(pack, itemType string, items []ItemAxes, order []string) error {
	rank := make(map[string]int, len(order))
	for i, name := range order {
		rank[name] = i
	}
	sort.SliceStable(items, func(i, j int) bool { return rank[items[i].Name] < rank[items[j].Name] })
	return WriteJSON(filepath.Join(pack, AxesFile), map[string][]ItemAxes{itemType: items})
}
