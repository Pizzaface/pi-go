package piext

import "os"

type stdinReadCloser struct{}

func (stdinReadCloser) Read(p []byte) (int, error) { return os.Stdin.Read(p) }
func (stdinReadCloser) Close() error               { return os.Stdin.Close() }

type stdoutWriteCloser struct{}

func (stdoutWriteCloser) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdoutWriteCloser) Close() error                { return os.Stdout.Close() }
