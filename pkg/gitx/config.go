package gitx

type xConfig struct{}

func (xConfig) Set(dir, key, value string) error {
	return In(dir).Cmd(kConfig, key, value).Err()
}

func (xConfig) Unset(dir, key string) error {
	code, err := In(dir).Cmd(kConfig, flagUnset, key).ExitCode()
	if err != nil {
		return err
	}
	// `git config --unset` exits 5 when the key wasn't set; treat as no-op.
	if code != 0 && code != 5 {
		return In(dir).Cmd(kConfig, flagUnset, key).Err()
	}
	return nil
}
