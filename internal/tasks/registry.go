package tasks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Parameter is a single literal positional argument, never shell source.
type Parameter struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
}

// Definition registers a trusted local executable. Category restricts the
// selected directory; Write requires a writable library and exclusive access.
// Executable/Args/Env are accepted only from the private startup file, not HTTP.
type Definition struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Category   string            `json:"category"`
	Write      bool              `json:"write"`
	Parameters []Parameter       `json:"parameters"`
	Executable string            `json:"executable"`
	Args       []string          `json:"args"`
	Env        map[string]string `json:"env,omitempty"`
}

// RegisteredTask is the browser-safe projection; credentials and commands stay local.
type RegisteredTask struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Category   string      `json:"category"`
	Write      bool        `json:"write"`
	Parameters []Parameter `json:"parameters"`
}

// Registry is immutable after startup; it performs no periodic reads or writes.
type Registry struct {
	definitions map[string]Definition
	public      []RegisteredTask
	problem     string
}

var identifier = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.-]{0,63}$`)
var environmentName = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)

// LoadRegistry reads optional configDir/tasks.json (64KiB, at most 32 jobs).
// Relative executables are anchored to configDir. Missing means no registered jobs.
func LoadRegistry(configDir string) (registry *Registry, resultErr error) {
	path := filepath.Join(configDir, "tasks.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return NewRegistry(nil, configDir)
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return nil, errors.New("task registry is not a bounded regular file")
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return NewRegistry(nil, configDir)
	}
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	info, err = file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return nil, errors.New("task registry is not a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 64<<10 {
		return nil, errors.New("task registry exceeds 64KiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var definitions []Definition
	if err = decoder.Decode(&definitions); err != nil {
		return nil, fmt.Errorf("task registry: %w", err)
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("task registry has trailing data")
	}
	for _, definition := range definitions {
		if len(definition.Env) > 0 && runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
			return nil, errors.New("task registry containing credentials must have private permissions")
		}
	}
	return NewRegistry(definitions, configDir)
}

// NewRegistry validates trusted definitions and resolves executables once.
func NewRegistry(definitions []Definition, base string) (*Registry, error) {
	if len(definitions) > 32 {
		return nil, errors.New("too many registered tasks")
	}
	r := &Registry{definitions: make(map[string]Definition), public: []RegisteredTask{}}
	for _, d := range definitions {
		if !identifier.MatchString(d.ID) || d.Name == "" || len(d.Name) > 128 || d.Executable == "" || len(d.Executable) > 4096 || strings.ContainsRune(d.Executable, 0) || len(d.Parameters) > 16 || len(d.Args) > 64 || len(d.Env) > 16 {
			return nil, errors.New("invalid registered task")
		}
		if _, exists := r.definitions[d.ID]; exists {
			return nil, errors.New("duplicate registered task")
		}
		category := filepath.ToSlash(filepath.Clean(d.Category))
		if d.Category != category || filepath.IsAbs(category) || category == "." || strings.HasPrefix(category, "../") || strings.Contains(category, "\\") || strings.ContainsRune(category, 0) || len(category) > 256 {
			return nil, errors.New("invalid task category")
		}
		seen := map[string]bool{}
		for _, p := range d.Parameters {
			if !identifier.MatchString(p.Name) || seen[p.Name] || p.Label == "" || len(p.Label) > 128 {
				return nil, errors.New("invalid task parameter")
			}
			seen[p.Name] = true
		}
		total := 0
		d.Args = append([]string(nil), d.Args...)
		for index, a := range d.Args {
			a = strings.ReplaceAll(a, "{config_dir}", base)
			d.Args[index] = a
			if len(a) > 4096 || strings.ContainsRune(a, 0) {
				return nil, errors.New("invalid fixed argument")
			}
			total += len(a)
		}
		env := make(map[string]string, len(d.Env))
		for k, v := range d.Env {
			if k == "OMNIHUB_TASK_DIR" || !environmentName.MatchString(k) || len(v) > 4096 || strings.ContainsRune(v, 0) {
				return nil, errors.New("invalid task environment")
			}
			total += len(k) + len(v)
			env[k] = v
		}
		if total > 32<<10 {
			return nil, errors.New("task arguments/environment exceed capacity")
		}
		executable := d.Executable
		if strings.ContainsAny(executable, "/\\") && !filepath.IsAbs(executable) {
			executable = filepath.Join(base, executable)
		}
		resolved, err := exec.LookPath(executable)
		if err != nil {
			return nil, fmt.Errorf("registered task %s executable unavailable", d.ID)
		}
		d.Executable, err = filepath.Abs(resolved)
		if err != nil {
			return nil, err
		}
		d.Args = append([]string(nil), d.Args...)
		d.Parameters = append([]Parameter{}, d.Parameters...)
		d.Env = env
		r.definitions[d.ID] = d
		r.public = append(r.public, RegisteredTask{d.ID, d.Name, d.Category, d.Write, d.Parameters})
	}
	return r, nil
}

// List returns independent safe metadata; it never reveals credential values.
func (r *Registry) List() []RegisteredTask {
	result := append([]RegisteredTask{}, r.public...)
	for i := range result {
		result[i].Parameters = append([]Parameter{}, result[i].Parameters...)
	}
	return result
}

// Prepare accepts only a known ID and its named form fields. Directory is a
// canonical library path resolved by the caller; cwd and OMNIHUB_TASK_DIR agree.
func (r *Registry) Prepare(id, directory string, values map[string]string) (Command, error) {
	d, ok := r.definitions[id]
	if !ok {
		return Command{}, errors.New("unregistered task")
	}
	if len(values) > len(d.Parameters) {
		return Command{}, errors.New("unknown task parameter")
	}
	args := append([]string{}, d.Args...)
	known := map[string]bool{}
	for _, p := range d.Parameters {
		v := values[p.Name]
		known[p.Name] = true
		if (p.Required && strings.TrimSpace(v) == "") || len(v) > 1024 || strings.ContainsRune(v, 0) {
			return Command{}, errors.New("invalid task parameter")
		}
		args = append(args, v)
	}
	for key := range values {
		if !known[key] {
			return Command{}, errors.New("unknown task parameter")
		}
	}
	env := []string{"OMNIHUB_TASK_DIR=" + directory}
	for k, v := range d.Env {
		if k == "OMNIHUB_TASK_DIR" {
			return Command{}, errors.New("reserved task environment")
		}
		env = append(env, k+"="+v)
	}
	return Command{Name: d.Name, Executable: d.Executable, Args: args, Directory: directory, Environment: env, Resource: directory, Write: d.Write}, nil
}

// Lookup returns safe task metadata for authorization before preparing a command.
func (r *Registry) Lookup(id string) (RegisteredTask, bool) {
	for _, d := range r.List() {
		if d.ID == id {
			return d, true
		}
	}
	return RegisteredTask{}, false
}

// DisabledRegistry preserves a startup error without disabling unrelated modules.
func DisabledRegistry(problem error) *Registry {
	return &Registry{definitions: map[string]Definition{}, public: []RegisteredTask{}, problem: problem.Error()}
}

// Error returns the startup configuration error, or empty when registration succeeded.
func (r *Registry) Error() string { return r.problem }
