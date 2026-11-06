package main

import "os"

func writeFile(path, s string) error { return os.WriteFile(path, []byte(s), 0o600) }
