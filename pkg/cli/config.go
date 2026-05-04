package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/ashi-labs/gg/pkg/config"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "print or write the default gg config.",
		Long: `with no flags, prints the default gg config (toml) to stdout.
useful for piping into a new config file or seeing every available
knob and its baseline value.

with -w/--write, writes the default config to the canonical path
($XDG_CONFIG_HOME/gg/config.toml, falling back to ~/.config/gg/config.toml).
refuses if a config already exists at that path; remove it first or
edit in place.

later subcommands (` + "`get`" + `, ` + "`set`" + `) will read and mutate individual keys.`,
		Args: cobra.NoArgs,
		RunE: runConfig,
	}
	cmd.Flags().BoolP("write", "w", false, "write the default config to the canonical path")
	return cmd
}

func runConfig(cmd *cobra.Command, args []string) error {
	write, _ := cmd.Flags().GetBool("write")
	body, err := encodeDefaultConfigTOML()
	if err != nil {
		return fmt.Errorf("rendering default config: %w", err)
	}
	if !write {
		stdout(body)
		return nil
	}
	path := config.DefaultPath()
	if _, err := os.Stat(path); err == nil {
		ok, err := confirmYesNo("config already exists. overwrite it?")
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("cancelled")
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	successf("wrote default config @ %s", out.palette.Dim.Render(path))
	return nil
}

// encodeDefaultConfigTOML renders config.Default() as TOML. The Path
// field is annotated `toml:"-"` so it never leaks into output, which
// matters here since the default Path is empty (no file loaded yet).
func encodeDefaultConfigTOML() (string, error) {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(config.Default()); err != nil {
		return "", err
	}
	return buf.String(), nil
}
