package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"
)

func newUpgradeCmd() *cobra.Command {
	var dir, after string
	cmd := &cobra.Command{
		Use:   "upgrade [version]",
		Short: "Move the app to a newer Tifa and bring what Tifa manages in the app along",
		Long: `upgrade moves the app to another release of Tifa (the latest when you give no version) and
then brings along what that release pins or manages. In order:

  1. go get -tool github.com/liustack/tifa/cmd/tifa@<version>, then go mod tidy.
  2. The rest is run by the new tifa, which knows its own pins: go tool tifa upgrade --after <old>.
  3. The versions of the packages that tifa new pins (react, react-dom, @tanstack/react-router,
     vite and the others of its package.json) are set in package.json when the app lists them,
     and nothing else in the file changes. Then the install command runs.
  4. The skill and the managed block of AGENTS.md are written again, as tifa agents does.
  5. The "Upgrading:" notes of the CHANGELOG entries after your old version, up to the new one, are
     printed with their versions. Nothing changes in your code: the notes say what to do by hand.
  6. tifa check runs, and its result is the result of upgrade.

It refuses a version older than the one the app has, and says so when the app already has it. It
never runs the old tifa for the later steps, and it does not forward to the tifa of the app, so it
also works in an app whose tifa is too old to have this command: run it with a tifa installed
globally, or go run github.com/liustack/tifa/cmd/tifa@latest upgrade.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			if after != "" {
				if len(args) > 0 {
					return errors.New("--after takes no version")
				}
				return upgradeFinish(cmd, root, after)
			}
			version := ""
			if len(args) > 0 {
				version = args[0]
			}
			return upgradeStart(cmd, root, version)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "application root, the directory holding go.mod and app/")
	// The first step starts the new tifa with this flag. It is not for people.
	cmd.Flags().StringVar(&after, "after", "", "the version the app had before: run the steps after go get")
	_ = cmd.Flags().MarkHidden("after")
	return cmd
}

// appTifaVersion returns the version of tifa that the go.mod in root requires.
func appTifaVersion(root string) (version string, replaced bool, err error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", false, fmt.Errorf("%s has no go.mod to read (%v): run tifa upgrade in the app root or pass --dir", root, err)
	}
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return "", false, fmt.Errorf("go.mod: %w", err)
	}
	for _, r := range f.Require {
		if r.Mod.Path == tifaModule {
			version = r.Mod.Version
		}
	}
	for _, r := range f.Replace {
		replaced = replaced || r.Old.Path == tifaModule
	}
	if version == "" {
		return "", false, fmt.Errorf("go.mod does not require %s, so this is not a Tifa app", tifaModule)
	}
	return version, replaced, nil
}

// moduleInfo is what go list -m -json prints for a module.
type moduleInfo struct {
	Version string
	Dir     string
}

// listModule runs go list -m -json for the module at query (tifa@latest, or tifa for the version the
// app requires) in root.
func listModule(root, query string) (moduleInfo, error) {
	cmd := exec.Command("go", "list", "-m", "-json", query)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return moduleInfo{}, fmt.Errorf("go list -m %s: %w\n%s", query, err, exit.Stderr)
		}
		return moduleInfo{}, fmt.Errorf("go list -m %s: %w", query, err)
	}
	var info moduleInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return moduleInfo{}, err
	}
	return info, nil
}

// upgradeStart moves go.mod to version, the latest release when it is "", then hands the rest to
// the new tifa.
func upgradeStart(cmd *cobra.Command, root, version string) error {
	current, replaced, err := appTifaVersion(root)
	if err != nil {
		return err
	}
	if replaced {
		return fmt.Errorf("go.mod replaces %s, so the version in it is not the one the app builds with. Remove the replace (go mod edit -dropreplace %s) and run tifa upgrade again", tifaModule, tifaModule)
	}
	if version == "" {
		latest, err := listModule(root, tifaModule+"@latest")
		if err != nil {
			return err
		}
		version = latest.Version
	}
	if err := checkUpgradeVersions(current, version); err != nil {
		return err
	}
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	fmt.Fprintf(out, "==> tifa %s to %s\n", current, version)
	for _, args := range [][]string{
		{"get", "-tool", toolPackage + "@" + version},
		{"mod", "tidy"},
	} {
		fmt.Fprintf(out, "==> go %s\n", strings.Join(args, " "))
		if err := runIn(root, out, errOut, nil, "go", args...); err != nil {
			return fmt.Errorf("go %s failed: %w\nBy hand: go get -tool %s@%s, go mod tidy, then go tool tifa upgrade --after %s", args[0], err, toolPackage, version, current)
		}
	}
	// go tool builds the tifa of the new go.mod, which is the only one that knows what it pins.
	// The marker keeps it from forwarding again.
	fmt.Fprintf(out, "==> go tool tifa upgrade --after %s\n", current)
	env := append(os.Environ(), forwardedEnv+"=1")
	if err := runIn(root, out, errOut, env, "go", "tool", "tifa", "upgrade", "--after", current); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() > 0 {
			return &exitError{code: exit.ExitCode()}
		}
		return fmt.Errorf("go tool tifa failed: %w\nBy hand: go tool tifa upgrade --after %s", err, current)
	}
	return nil
}

// runIn runs name with args in root. env is the environment, or nil for this process's.
func runIn(root string, stdout, stderr io.Writer, env []string, name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Dir = root
	c.Env = env
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, stdout, stderr
	return c.Run()
}

// upgradeFinish does what comes after go get, as the tifa that the app now requires. old is the
// version the app had.
func upgradeFinish(cmd *cobra.Command, root, old string) error {
	current, _, err := appTifaVersion(root)
	if err != nil {
		return err
	}
	if err := checkUpgradeVersions(old, current); err != nil {
		return fmt.Errorf("--after %s: %w", old, err)
	}
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	fmt.Fprintln(out, "==> package.json")
	if err := upgradePackages(root, out, errOut); err != nil {
		return fmt.Errorf("package.json: %w", err)
	}
	fmt.Fprintln(out, "==> agent files")
	plan, err := planAgents(root, defaultSkillsDir)
	if err != nil {
		return err
	}
	if len(plan.files) == 0 {
		fmt.Fprintln(out, "AGENTS.md and "+defaultSkillsDir+" are up to date")
	}
	for _, c := range plan.files {
		if err := c.apply(root); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s: %s\n", c.rel, c.what)
	}
	fmt.Fprintf(out, "==> Upgrading notes, %s to %s\n", old, current)
	printUpgradeNotes(root, old, current, out)
	return runCheck(cmd, root)
}

// upgradePackages sets the pinned versions in the package.json of root and installs, with the
// command of the lock file the app has. An app without package.json has no pages and nothing to do.
func upgradePackages(root string, out, errOut io.Writer) error {
	file := filepath.Join(root, "package.json")
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(out, "no package.json, nothing to set")
		return nil
	} else if err != nil {
		return err
	}
	pins, err := pinnedPackages()
	if err != nil {
		return err
	}
	updated, changes, err := setPins(data, pins)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		fmt.Fprintln(out, "the pinned packages are already at the versions of this Tifa")
	}
	for _, c := range changes {
		fmt.Fprintf(out, "%s: %s -> %s\n", c.Name, c.From, c.To)
	}
	if len(changes) > 0 {
		if err := os.WriteFile(file, updated, 0o644); err != nil {
			return err
		}
	}
	install, _, notes := installCommand(root, nil)
	for _, n := range notes {
		fmt.Fprintf(out, "note: %s\n", n)
	}
	fmt.Fprintf(out, "==> %s\n", install)
	fields := strings.Fields(install)
	if err := runIn(root, out, errOut, nil, fields[0], fields[1:]...); err != nil {
		return fmt.Errorf("%s failed: %w", install, err)
	}
	return nil
}

// printUpgradeNotes prints the "Upgrading:" notes of the CHANGELOG.md of the tifa module the app
// now uses, for the versions after old up to current.
func printUpgradeNotes(root, old, current string, out io.Writer) {
	info, err := listModule(root, tifaModule)
	if err == nil {
		var data []byte
		if data, err = os.ReadFile(filepath.Join(info.Dir, "CHANGELOG.md")); err == nil {
			notes := upgradingNotes(string(data), old, current)
			if len(notes) == 0 {
				fmt.Fprintln(out, "none: no entry after "+old+" has an Upgrading note")
			}
			for _, n := range notes {
				fmt.Fprintf(out, "%s: %s\n", n.Version, n.Text)
			}
			return
		}
	}
	fmt.Fprintf(out, "note: could not read the CHANGELOG.md of %s %s (%v). It is at https://github.com/liustack/tifa/blob/%s/CHANGELOG.md\n", tifaModule, current, err, current)
}
